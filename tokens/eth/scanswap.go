package eth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	ethclient "github.com/jowenshaw/gethclient"
	"github.com/jowenshaw/gethclient/common"
	"github.com/jowenshaw/gethclient/types"

	"github.com/weijun-sh/gethscan-server/log"
	"github.com/weijun-sh/gethscan-server/params"
	"github.com/weijun-sh/gethscan-server/tools"
	"github.com/weijun-sh/gethscan-server/tokens"
	"github.com/weijun-sh/gethscan-server/mongodb"

)

var (
	transferFuncHash       = common.FromHex("0xa9059cbb")
	transferFromFuncHash   = common.FromHex("0x23b872dd")
	addressSwapoutFuncHash = common.FromHex("0x628d6cba") // for ETH like `address` type address
	stringSwapoutFuncHash  = common.FromHex("0xad54056d") // for BTC like `string` type address

	transferLogTopic       = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	addressSwapoutLogTopic = common.HexToHash("0x6b616089d04950dc06c45c6dd787d657980543f89651aec47924752c7d16c888")
	stringSwapoutLogTopic  = common.HexToHash("0x9c92ad817e5474d30a4378deface765150479363a897b0590fbb12ae9d89396b")

	routerAnySwapOutTopic                  = common.FromHex("0x97116cf6cd4f6412bb47914d6db18da9e16ab2142f543b86e207c24fbd16b23a")
	routerAnySwapTradeTokensForTokensTopic = common.FromHex("0xfea6abdf4fd32f20966dff7619354cd82cd43dc78a3bee479f04c74dbfc585b3")
	routerAnySwapTradeTokensForNativeTopic = common.FromHex("0x278277e0209c347189add7bd92411973b5f6b8644f7ac62ea1be984ce993f8f4")

	logNFT721SwapOutTopic       = common.FromHex("0x0d45b0b9f5add3e1bb841982f1fa9303628b0b619b000cb1f9f1c3903329a4c7")
	logNFT1155SwapOutTopic      = common.FromHex("0x5058b8684cf36ffd9f66bc623fbc617a44dd65cf2273306d03d3104af0995cb0")
	logNFT1155SwapOutBatchTopic = common.FromHex("0xaa428a5ab688b49b415401782c170d216b33b15711d30cf69482f570eca8db38")

	logAnycallSwapOutTopic = common.FromHex("0x3d1b3d059223895589208a5541dce543eab6d5942b3b1129231a942d1c47bc45")
	logAnycallTransferSwapOutTopic = common.FromHex("0xcaac11c45e5fdb5c513e20ac229a3f9f99143580b5eb08d0fecbdd5ae8c81ef5")
)

const (
	postSwapSuccessResult   = "success"
	bridgeSwapExistKeywords = "mgoError: Item is duplicate"
	routerSwapExistResult   = "already registered"
	routerSwapExistResultTmp   = "alreday registered"
	httpTimeoutKeywords     = "Client.Timeout exceeded while awaiting headers"
	errConnectionRefused    = "connect: connection refused"
	errMaximumRequestLimit  = "You have reached maximum request limit"
	rpcQueryErrKeywords     = "rpc query error"
	errDepositLogNotFountorRemoved = "return error: json-rpc error -32099, verify swap failed! deposit log not found or removed"
)

var (
	chainScanner map[string]*ethSwapScanner = make(map[string]*ethSwapScanner)
)

type ethSwapScanner struct {
        gateway     string
	scanReceipt bool

        chainID *big.Int
	chain string

        client *ethclient.Client
        ctx    context.Context

        rpcInterval   time.Duration
        rpcRetryCount int

        cachedSwapPosts *tools.Ring
        tokens []*params.TokenConfig
}

func InitCrossChain() {
	params.InitCrossChain()
	config := params.GetScanTokensConfig()
	for chain, scantoken := range config {
		scanner := buildChain(chain, scantoken)
		chainScanner[chain] = scanner
	}
}

func buildChain(chain string, scantoken *params.ScanTokensConfig) *ethSwapScanner {
        scanner := &ethSwapScanner{
                ctx:           context.Background(),
                rpcInterval:   1 * time.Second,
                rpcRetryCount: 3,
        }
        scanner.gateway = params.GetChainRPC(chain)
	scanner.chain = chain
	scanner.tokens = scantoken.Tokens

        log.Info("get argument success",
		"chain", chain,
                "gateway", scanner.gateway,
        )

        scanner.initClient()
	return scanner
}

func (scanner *ethSwapScanner) initClient() {
        ethcli, err := ethclient.Dial(scanner.gateway)
        if err != nil {
                log.Fatal("ethclient.Dail failed", "gateway", scanner.gateway, "err", err)
        }
        log.Info("ethclient.Dail gateway success", "gateway", scanner.gateway)
        scanner.client = ethcli
        scanner.chainID, err = ethcli.ChainID(scanner.ctx)
        if err != nil {
                log.Fatal("get chainID failed", "err", err)
        }
        log.Info("get chainID success", "chainID", scanner.chainID)
}

func GetChainScanner(chain string) *ethSwapScanner {
	return chainScanner[chain]
}

type swapPost struct {
	// common
	txid       string
	rpcMethod  string
	swapServer string

	// bridge
	pairID string

	// router
	chainID  string
	logIndex string
}

func (scanner *ethSwapScanner) loopGetLatestBlockNumber() uint64 {
	for { // retry until success
		header, err := scanner.client.HeaderByNumber(scanner.ctx, nil)
		if err == nil {
			log.Info("get latest block number success", "height", header.Number)
			return header.Number.Uint64()
		}
		log.Warn("get latest block number failed", "err", err)
		time.Sleep(scanner.rpcInterval)
	}
}

func (scanner *ethSwapScanner) loopGetTx(txHash common.Hash) (tx *types.Transaction, err error) {
	for i := 0; i < 5; i++ { // with retry
		tx, _, err = scanner.client.TransactionByHash(scanner.ctx, txHash)
		if err == nil {
			log.Debug("loopGetTx found", "tx", tx)
			return tx, nil
		}
		time.Sleep(scanner.rpcInterval)
	}
	return nil, err
}

func (scanner *ethSwapScanner) loopGetTxReceipt(txHash common.Hash) (receipt *types.Receipt, err error) {
	for i := 0; i < 5; i++ { // with retry
		receipt, err = scanner.client.TransactionReceipt(scanner.ctx, txHash)
		if err == nil {
			if receipt.Status != 1 {
				log.Debug("tx with wrong receipt status", "txHash", txHash.Hex())
				return nil, errors.New("tx with wrong receipt status")
			}
			return receipt, nil
		}
		time.Sleep(scanner.rpcInterval)
	}
	return nil, err
}

func (scanner *ethSwapScanner) loopGetBlock(height uint64) (block *types.Block, err error) {
	blockNumber := new(big.Int).SetUint64(height)
	for i := 0; i < 5; i++ { // with retry
		block, err = scanner.client.BlockByNumber(scanner.ctx, blockNumber)
		if err == nil {
			return block, nil
		}
		log.Warn("get block failed", "height", height, "err", err)
		time.Sleep(scanner.rpcInterval)
	}
	return nil, err
}

func (scanner *ethSwapScanner) scanTransaction(txid string) error {
	tx, err := scanner.loopGetTx(common.HexToHash(txid))
	if err != nil {
		log.Info("tx not found", "txid", txid)
		mongodb.UpdateSwapPendingNotFound(txid)
		return errors.New("tx not found")
	}
	if tx.To() == nil {
		log.Info("tx to is null", "txid", txid)
		mongodb.UpdateSwapPendingNotFound(txid)
		return errors.New("tx to is null")
	}

	for _, tokenCfg := range scanner.tokens {
		err = scanner.verifyTransaction(txid, tx, tokenCfg)
		if err == nil {
			mongodb.UpdateSwapPendingSuccess(txid)
			return nil
		}
	}
	mongodb.UpdateSwapPendingFailed(txid)
	log.Debug("verify tx failed", "txHash", txid, "err", err)
	return err
}

func (scanner *ethSwapScanner) checkTxToAddress(tx *types.Transaction, tokenCfg *params.TokenConfig) (receipt *types.Receipt, isAcceptToAddr bool) {
	needReceipt := scanner.scanReceipt
	txtoAddress := tx.To().String()

	var cmpTxTo string
	if tokenCfg.IsRouterSwap() {
		cmpTxTo = tokenCfg.RouterContract
		needReceipt = true
	} else if tokenCfg.IsNativeToken() {
		cmpTxTo = tokenCfg.DepositAddress
	} else {
		cmpTxTo = tokenCfg.TokenAddress
		if tokenCfg.CallByContract != "" {
			cmpTxTo = tokenCfg.CallByContract
			needReceipt = true
		}
	}

	if strings.EqualFold(txtoAddress, cmpTxTo) {
		isAcceptToAddr = true
	} else if !tokenCfg.IsNativeToken() {
		for _, whiteAddr := range tokenCfg.Whitelist {
			if strings.EqualFold(txtoAddress, whiteAddr) {
				isAcceptToAddr = true
				needReceipt = true
				break
			}
		}
	}

	if !isAcceptToAddr {
		return nil, false
	}

	if needReceipt {
		r, err := scanner.loopGetTxReceipt(tx.Hash())
		if err != nil {
			log.Warn("get tx receipt error", "txHash", tx.Hash().Hex(), "err", err)
			return nil, false
		}
		receipt = r
	}

	return receipt, true
}

func (scanner *ethSwapScanner) verifyTransaction(txid string, tx *types.Transaction, tokenCfg *params.TokenConfig) (verifyErr error) {
	receipt, isAcceptToAddr := scanner.checkTxToAddress(tx, tokenCfg)
	if !isAcceptToAddr {
		return tokens.ErrTxWithWrongReceiver
	}

	switch {
	// router swap
	case tokenCfg.IsRouterSwap():
		index := 0
		index, verifyErr = scanner.verifyAndPostRouterSwapTx(tx, receipt, tokenCfg)
		if verifyErr == nil {
			scanner.addRegisgerRouter(txid, index, tokenCfg)
		}
		return verifyErr

	// bridge swapin
	case tokenCfg.DepositAddress != "":
		if tokenCfg.IsNativeToken() {
			verifyErr = nil
			break
		}

		verifyErr = scanner.verifyErc20SwapinTx(tx, receipt, tokenCfg)

	// bridge swapout
	default:
		if scanner.scanReceipt {
			verifyErr = scanner.parseSwapoutTxLogs(receipt.Logs, tokenCfg)
		} else {
			verifyErr = scanner.verifySwapoutTx(tx, receipt, tokenCfg)
		}
	}

	if verifyErr == nil {
		scanner.addRegisterSwap(txid, tokenCfg) // TODO
	}
	return verifyErr
}

func (scanner *ethSwapScanner) addRegisterSwap(txid string, tokenCfg *params.TokenConfig) {
        pairID := tokenCfg.PairID
        var subject, rpcMethod string
        if tokenCfg.DepositAddress != "" {
                subject = "add bridge swapin register"
                rpcMethod = "swap.Swapin"
        } else {
                subject = "add bridge swapout register"
                rpcMethod = "swap.Swapout"
        }
        log.Info(subject, "txid", txid, "pairID", pairID)
	mongodb.AddRegisteredSwap(scanner.chain, rpcMethod, pairID, txid, "0", "0", tokenCfg.SwapServer)
	mongodb.UpdateSwapPendingSuccess(txid)
}

func (scanner *ethSwapScanner) addRegisgerRouter(txid string, logIndex int, tokenCfg *params.TokenConfig) {
        chainID := tokenCfg.ChainID

        subject := "add swap router register"
        rpcMethod := "swap.RegisterRouterSwap"
        log.Info(subject, "chainid", chainID, "txid", txid, "logindex", logIndex)
	mongodb.AddRegisteredSwap(scanner.chain, rpcMethod, "", txid, chainID, fmt.Sprintf("%v", logIndex), tokenCfg.SwapServer)
}

func (scanner *ethSwapScanner) getSwapoutFuncHashByTxType(txType string) []byte {
	switch strings.ToLower(txType) {
	case params.TxSwapout:
		return addressSwapoutFuncHash
	case params.TxSwapout2:
		return stringSwapoutFuncHash
	default:
		log.Errorf("unknown swapout tx type %v", txType)
		return nil
	}
}

func (scanner *ethSwapScanner) getLogTopicByTxType(txType string) (topTopic common.Hash, topicsLen int) {
	switch strings.ToLower(txType) {
	case params.TxSwapin:
		return transferLogTopic, 3
	case params.TxSwapout:
		return addressSwapoutLogTopic, 3
	case params.TxSwapout2:
		return stringSwapoutLogTopic, 2
	default:
		log.Errorf("unknown tx type %v", txType)
		return common.Hash{}, 0
	}
}

func (scanner *ethSwapScanner) verifyErc20SwapinTx(tx *types.Transaction, receipt *types.Receipt, tokenCfg *params.TokenConfig) (err error) {
	if receipt == nil {
		err = scanner.parseErc20SwapinTxInput(tx.Data(), tokenCfg.DepositAddress)
	} else {
		err = scanner.parseErc20SwapinTxLogs(receipt.Logs, tokenCfg)
	}
	return err
}

func (scanner *ethSwapScanner) verifySwapoutTx(tx *types.Transaction, receipt *types.Receipt, tokenCfg *params.TokenConfig) (err error) {
	if receipt == nil {
		err = scanner.parseSwapoutTxInput(tx.Data(), tokenCfg.TxType)
	} else {
		err = scanner.parseSwapoutTxLogs(receipt.Logs, tokenCfg)
	}
	return err
}

func (scanner *ethSwapScanner) verifyAndPostRouterSwapTx(tx *types.Transaction, receipt *types.Receipt, tokenCfg *params.TokenConfig) (int, error) {
	if receipt == nil {
		return 0, tokens.ErrTxReceiptNotFound
	}
	for i := 0; i < len(receipt.Logs); i++ {
		rlog := receipt.Logs[i]
		if rlog.Removed {
			continue
		}
		if !strings.EqualFold(rlog.Address.String(), tokenCfg.RouterContract) {
			continue
		}
		logTopic := rlog.Topics[0].Bytes()
		switch {
		case tokenCfg.IsRouterERC20Swap():
			switch {
			case bytes.Equal(logTopic, routerAnySwapOutTopic):
			case bytes.Equal(logTopic, routerAnySwapTradeTokensForTokensTopic):
			case bytes.Equal(logTopic, routerAnySwapTradeTokensForNativeTopic):
			default:
				continue
			}
		case tokenCfg.IsRouterNFTSwap():
			switch {
			case bytes.Equal(logTopic, logNFT721SwapOutTopic):
			case bytes.Equal(logTopic, logNFT1155SwapOutTopic):
			case bytes.Equal(logTopic, logNFT1155SwapOutBatchTopic):
			default:
				continue
			}
		case tokenCfg.IsRouterAnycallSwap():
			switch {
			case bytes.Equal(logTopic, logAnycallSwapOutTopic):
			case bytes.Equal(logTopic, logAnycallTransferSwapOutTopic):
			default:
				continue
			}
		}
		return i, nil
	}
	return 0, tokens.ErrRouterLogNotFound
}

func (scanner *ethSwapScanner) parseErc20SwapinTxInput(input []byte, depositAddress string) error {
	if len(input) < 4 {
		return tokens.ErrTxWithWrongInput
	}
	var receiver string
	funcHash := input[:4]
	switch {
	case bytes.Equal(funcHash, transferFuncHash):
		receiver = common.BytesToAddress(common.GetData(input, 4, 32)).Hex()
	case bytes.Equal(funcHash, transferFromFuncHash):
		receiver = common.BytesToAddress(common.GetData(input, 36, 32)).Hex()
	default:
		return tokens.ErrTxFuncHashMismatch
	}
	if !strings.EqualFold(receiver, depositAddress) {
		return tokens.ErrTxWithWrongReceiver
	}
	return nil
}

func (scanner *ethSwapScanner) parseErc20SwapinTxLogs(logs []*types.Log, tokenCfg *params.TokenConfig) (err error) {
	targetContract := tokenCfg.TokenAddress
	depositAddress := tokenCfg.DepositAddress
	cmpLogTopic, topicsLen := scanner.getLogTopicByTxType(tokenCfg.TxType)

	transferLogExist := false
	for _, rlog := range logs {
		if rlog.Removed {
			continue
		}
		if !strings.EqualFold(rlog.Address.Hex(), targetContract) {
			continue
		}
		if len(rlog.Topics) != topicsLen || rlog.Data == nil {
			continue
		}
		if rlog.Topics[0] != cmpLogTopic {
			continue
		}
		transferLogExist = true
		receiver := common.BytesToAddress(rlog.Topics[2][:]).Hex()
		if strings.EqualFold(receiver, depositAddress) {
			return nil
		}
	}
	if transferLogExist {
		fmt.Printf("parseErc20SwapinTxLogs, transferLogExist: %v\n", transferLogExist)
		return tokens.ErrTxWithWrongReceiver
	}
	fmt.Printf("parseErc20SwapinTxLogs, tokens.ErrDepositLogNotFound\n")
	return tokens.ErrDepositLogNotFound
}

func (scanner *ethSwapScanner) parseSwapoutTxInput(input []byte, txType string) error {
	if len(input) < 4 {
		return tokens.ErrTxWithWrongInput
	}
	funcHash := input[:4]
	if bytes.Equal(funcHash, scanner.getSwapoutFuncHashByTxType(txType)) {
		return nil
	}
	return tokens.ErrTxFuncHashMismatch
}

func (scanner *ethSwapScanner) parseSwapoutTxLogs(logs []*types.Log, tokenCfg *params.TokenConfig) (err error) {
	targetContract := tokenCfg.TokenAddress
	cmpLogTopic, topicsLen := scanner.getLogTopicByTxType(tokenCfg.TxType)

	for _, rlog := range logs {
		if rlog.Removed {
			continue
		}
		if !strings.EqualFold(rlog.Address.Hex(), targetContract) {
			continue
		}
		if len(rlog.Topics) != topicsLen || rlog.Data == nil {
			continue
		}
		if rlog.Topics[0] == cmpLogTopic {
			return nil
		}
	}
	return tokens.ErrSwapoutLogNotFound
}

type cachedSacnnedBlocks struct {
	capacity  int
	nextIndex int
	hashes    []string
}

var cachedBlocks = &cachedSacnnedBlocks{
	capacity:  100,
	nextIndex: 0,
	hashes:    make([]string, 100),
}

func (cache *cachedSacnnedBlocks) addBlock(blockHash string) {
	cache.hashes[cache.nextIndex] = blockHash
	cache.nextIndex = (cache.nextIndex + 1) % cache.capacity
}

func (cache *cachedSacnnedBlocks) isScanned(blockHash string) bool {
	for _, b := range cache.hashes {
		if b == blockHash {
			return true
		}
	}
	return false
}

func FindSwapPendingAndRegister() {
        pending, err := mongodb.FindSwapPending("", 0, 10)
        if err != nil || len(pending) == 0 {
               return
	}
        wg := new(sync.WaitGroup)
        wg.Add(len(pending))

	for i, _ := range pending {
		go func(p *mongodb.MgoRegisteredSwapPending) {
			defer wg.Done()
			chain := p.Chain
			txid := p.Key
			ParseTx(chain, txid)
			scanner := GetChainScanner(chain)
			if scanner == nil {
				log.Info("FindSwapPendingAndRegister", "txid", txid, "(not set rpc)chain", chain)
				return
			}
			log.Info("FindSwapPendingAndRegister", "txid", txid, "chain", chain)
			scanner.scanTransaction(txid)
		}(pending[i])
	}
	wg.Wait()
}

func ParseTx(chain, txid string) error {
	scanner := GetChainScanner(chain)
	if scanner == nil {
		log.Info("ParseTx", "txid", txid, "(not set rpc)chain", chain)
		return errors.New("(not set rpc)chain")
	}
	log.Info("ParseTx", "txid", txid, "chain", chain)
	return scanner.scanTransaction(txid)
}
