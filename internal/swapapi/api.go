package swapapi

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/btcsuite/btcd/txscript"
	rpcjson "github.com/gorilla/rpc/v2/json2"
	"github.com/weijun-sh/gethscan-server/log"
	"github.com/weijun-sh/gethscan-server/mongodb"
	"github.com/weijun-sh/gethscan-server/params"
	"github.com/weijun-sh/gethscan-server/worker"
	"github.com/weijun-sh/gethscan-server/tokens"
	"github.com/weijun-sh/gethscan-server/tokens/eth"
	"github.com/weijun-sh/gethscan-server/tokens/btc"
)

var (
	errNotBtcBridge      = newRPCError(-32096, "bridge is not btc")
	errTokenPairNotExist = newRPCError(-32095, "token pair not exist")
	errSwapCannotRetry   = newRPCError(-32094, "swap can not retry")
)

func newRPCError(ec rpcjson.ErrorCode, message string) error {
	return &rpcjson.Error{
		Code:    ec,
		Message: message,
	}
}

func newRPCInternalError(err error) error {
	return newRPCError(-32000, "rpcError: "+err.Error())
}

// GetServerInfo api
func GetServerInfo() (*ServerInfo, error) {
	log.Debug("[api] receive GetServerInfo")
	config := params.GetConfig()
	if config == nil {
		return nil, nil
	}
	return &ServerInfo{
		Identifier:          config.Identifier,
		MustRegisterAccount: params.MustRegisterAccount(),
		SrcChain:            config.SrcChain,
		DestChain:           config.DestChain,
		PairIDs:             tokens.GetAllPairIDs(),
		Version:             params.VersionWithMeta,
	}, nil
}

// GetTokenPairInfo api
func GetTokenPairInfo(pairID string) (*tokens.TokenPairConfig, error) {
	pairCfg := tokens.GetTokenPairConfig(pairID)
	if pairCfg == nil {
		return nil, errTokenPairNotExist
	}
	return pairCfg, nil
}

// GetTokenPairsInfo api
func GetTokenPairsInfo(pairIDs string) (map[string]*tokens.TokenPairConfig, error) {
	var pairIDSlice []string
	if strings.EqualFold(pairIDs, "all") {
		pairIDSlice = tokens.GetAllPairIDs()
	} else {
		pairIDSlice = strings.Split(pairIDs, ",")
	}
	result := make(map[string]*tokens.TokenPairConfig, len(pairIDSlice))
	for _, pairID := range pairIDSlice {
		result[pairID] = tokens.GetTokenPairConfig(pairID)
	}
	return result, nil
}

// GetNonceInfo api
func GetNonceInfo() (*SwapNonceInfo, error) {
	swapinNonces, swapoutNonces := mongodb.LoadAllSwapNonces()
	return &SwapNonceInfo{
		SwapinNonces:  swapinNonces,
		SwapoutNonces: swapoutNonces,
	}, nil
}

// GetSwapStatistics api
func GetSwapStatistics(pairID string) (*SwapStatistics, error) {
	log.Debug("[api] receive GetSwapStatistics", "pairID", pairID)
	return mongodb.GetSwapStatistics(pairID)
}

// GetRawSwapin api
func GetRawSwapin(txid, pairID, bindAddr *string) (*Swap, error) {
	return mongodb.FindSwapin(*txid, *pairID, *bindAddr)
}

// GetRawSwapinResult api
func GetRawSwapinResult(txid, pairID, bindAddr *string) (*SwapResult, error) {
	return mongodb.FindSwapinResult(*txid, *pairID, *bindAddr)
}

// GetSwapin api
func GetSwapin(txid, pairID, bindAddr *string) (*SwapInfo, error) {
	txidstr := *txid
	pairIDStr := *pairID
	bindStr := *bindAddr
	result, err := mongodb.FindSwapinResult(txidstr, pairIDStr, bindStr)
	if err == nil {
		return ConvertMgoSwapResultToSwapInfo(result), nil
	}
	register, err := mongodb.FindSwapin(txidstr, pairIDStr, bindStr)
	if err == nil {
		return ConvertMgoSwapToSwapInfo(register), nil
	}
	return nil, mongodb.ErrSwapNotFound
}

// GetRawSwapout api
func GetRawSwapout(txid, pairID, bindAddr *string) (*Swap, error) {
	return mongodb.FindSwapout(*txid, *pairID, *bindAddr)
}

// GetRawSwapoutResult api
func GetRawSwapoutResult(txid, pairID, bindAddr *string) (*SwapResult, error) {
	return mongodb.FindSwapoutResult(*txid, *pairID, *bindAddr)
}

// GetSwapout api
func GetSwapout(txid, pairID, bindAddr *string) (*SwapInfo, error) {
	txidstr := *txid
	pairIDStr := *pairID
	bindStr := *bindAddr
	result, err := mongodb.FindSwapoutResult(txidstr, pairIDStr, bindStr)
	if err == nil {
		return ConvertMgoSwapResultToSwapInfo(result), nil
	}
	register, err := mongodb.FindSwapout(txidstr, pairIDStr, bindStr)
	if err == nil {
		return ConvertMgoSwapToSwapInfo(register), nil
	}
	return nil, mongodb.ErrSwapNotFound
}

func processHistoryLimit(limit int) int {
	switch {
	case limit == 0:
		limit = 20 // default
	case limit > 100:
		limit = 100
	case limit < -100:
		limit = -100
	}
	return limit
}

// GetSwapinHistory api
func GetSwapinHistory(address, pairID string, offset, limit int, status string) ([]*SwapInfo, error) {
	log.Debug("[api] receive GetSwapinHistory", "address", address, "pairID", pairID, "offset", offset, "limit", limit, "status", status)
	limit = processHistoryLimit(limit)
	result, err := mongodb.FindSwapinResults(address, pairID, offset, limit, status)
	if err != nil {
		return nil, err
	}
	return ConvertMgoSwapResultsToSwapInfos(result), nil
}

// GetSwapoutHistory api
func GetSwapoutHistory(address, pairID string, offset, limit int, status string) ([]*SwapInfo, error) {
	log.Debug("[api] receive GetSwapoutHistory", "address", address, "pairID", pairID, "offset", offset, "limit", limit)
	limit = processHistoryLimit(limit)
	result, err := mongodb.FindSwapoutResults(address, pairID, offset, limit, status)
	if err != nil {
		return nil, err
	}
	return ConvertMgoSwapResultsToSwapInfos(result), nil
}

// Swapin api
func Swapin(txid, pairID *string) (*PostResult, error) {
	log.Debug("[api] receive Swapin", "txid", *txid, "pairID", *pairID)
	return swap(txid, pairID, true)
}

// RetrySwapin api
func RetrySwapin(txid, pairID *string) (*PostResult, error) {
	log.Debug("[api] retry Swapin", "txid", *txid, "pairID", *pairID)
	if _, ok := tokens.SrcBridge.(tokens.NonceSetter); !ok {
		return nil, errSwapCannotRetry
	}
	txidstr := *txid
	pairIDStr := *pairID
	if err := basicCheckSwapRegister(tokens.SrcBridge, pairIDStr); err != nil {
		return nil, err
	}
	swapInfo, err := tokens.SrcBridge.VerifyTransaction(pairIDStr, txidstr, true)
	if err != nil {
		return nil, newRPCError(-32099, "retry swapin failed! "+err.Error())
	}
	bindStr := swapInfo.Bind
	swap, _ := mongodb.FindSwapin(txidstr, pairIDStr, bindStr)
	if swap == nil {
		return nil, mongodb.ErrItemNotFound
	}
	if !swap.Status.CanRetry() {
		return nil, errSwapCannotRetry
	}
	err = mongodb.UpdateSwapinStatus(txidstr, pairIDStr, bindStr, mongodb.TxNotStable, time.Now().Unix(), "")
	if err != nil {
		return nil, err
	}
	return &SuccessPostResult, nil
}

// Swapout api
func Swapout(txid, pairID *string) (*PostResult, error) {
	log.Debug("[api] receive Swapout", "txid", *txid, "pairID", *pairID)
	return swap(txid, pairID, false)
}

func basicCheckSwapRegister(bridge tokens.CrossChainBridge, pairIDStr string) error {
	tokenCfg := bridge.GetTokenConfig(pairIDStr)
	if tokenCfg == nil {
		return tokens.ErrUnknownPairID
	}
	if tokenCfg.DisableSwap {
		return tokens.ErrSwapIsClosed
	}
	return nil
}

func swap(txid, pairID *string, isSwapin bool) (*PostResult, error) {
	txidstr := *txid
	pairIDStr := *pairID
	bridge := tokens.GetCrossChainBridge(isSwapin)
	if err := basicCheckSwapRegister(bridge, pairIDStr); err != nil {
		return nil, err
	}
	swapInfo, err := bridge.VerifyTransaction(pairIDStr, txidstr, true)
	var txType tokens.SwapTxType
	if isSwapin {
		txType = tokens.SwapinTx
	} else {
		txType = tokens.SwapoutTx
	}
	err = addSwapToDatabase(txidstr, txType, swapInfo, err)
	if err != nil {
		return nil, err
	}
	if isSwapin {
		log.Info("[api] receive swapin register", "txid", txidstr, "pairID", pairIDStr)
	} else {
		log.Info("[api] receive swapout register", "txid", txidstr, "pairID", pairIDStr)
	}
	return &SuccessPostResult, nil
}

func addSwapToDatabase(txid string, txType tokens.SwapTxType, swapInfo *tokens.TxSwapInfo, verifyError error) (err error) {
	if !tokens.ShouldRegisterSwapForError(verifyError) {
		return newRPCError(-32099, "verify swap failed! "+verifyError.Error())
	}
	var memo string
	if verifyError != nil {
		memo = verifyError.Error()
	}
	swap := &mongodb.MgoSwap{
		PairID:    swapInfo.PairID,
		TxID:      txid,
		TxTo:      swapInfo.TxTo,
		TxType:    uint32(txType),
		Bind:      swapInfo.Bind,
		Status:    mongodb.GetStatusByTokenVerifyError(verifyError),
		Timestamp: time.Now().Unix(),
		Memo:      memo,
	}
	isSwapin := txType == tokens.SwapinTx
	log.Info("[api] add swap", "isSwapin", isSwapin, "swap", swap)
	if isSwapin {
		err = mongodb.AddSwapin(swap)
	} else {
		err = mongodb.AddSwapout(swap)
	}
	return err
}

// IsValidSwapinBindAddress api
func IsValidSwapinBindAddress(address *string) bool {
	return tokens.DstBridge.IsValidAddress(*address)
}

// IsValidSwapoutBindAddress api
func IsValidSwapoutBindAddress(address *string) bool {
	return tokens.SrcBridge.IsValidAddress(*address)
}

// RegisterP2shAddress api
func RegisterP2shAddress(bindAddress string) (*tokens.P2shAddressInfo, error) {
	return calcP2shAddress(bindAddress, true)
}

// GetP2shAddressInfo api
func GetP2shAddressInfo(p2shAddress string) (*tokens.P2shAddressInfo, error) {
	bindAddress, err := mongodb.FindP2shBindAddress(p2shAddress)
	if err != nil {
		return nil, err
	}
	return calcP2shAddress(bindAddress, false)
}

func calcP2shAddress(bindAddress string, addToDatabase bool) (*tokens.P2shAddressInfo, error) {
	if btc.BridgeInstance == nil {
		return nil, errNotBtcBridge
	}
	p2shAddr, redeemScript, err := btc.BridgeInstance.GetP2shAddress(bindAddress)
	if err != nil {
		return nil, newRPCInternalError(err)
	}
	disasm, err := txscript.DisasmString(redeemScript)
	if err != nil {
		return nil, newRPCInternalError(err)
	}
	if addToDatabase {
		result, _ := mongodb.FindP2shAddress(bindAddress)
		if result == nil {
			_ = mongodb.AddP2shAddress(&mongodb.MgoP2shAddress{
				Key:         bindAddress,
				P2shAddress: p2shAddr,
			})
		}
	}
	return &tokens.P2shAddressInfo{
		BindAddress:        bindAddress,
		P2shAddress:        p2shAddr,
		RedeemScript:       hex.EncodeToString(redeemScript),
		RedeemScriptDisasm: disasm,
	}, nil
}

// P2shSwapin api
func P2shSwapin(txid, bindAddr *string) (*PostResult, error) {
	log.Debug("[api] receive P2shSwapin", "txid", *txid, "bindAddress", *bindAddr)
	if btc.BridgeInstance == nil {
		return nil, errNotBtcBridge
	}
	txidstr := *txid
	pairID := btc.PairID
	if swap, _ := mongodb.FindSwapin(txidstr, pairID, *bindAddr); swap != nil {
		return nil, mongodb.ErrItemIsDup
	}
	if err := basicCheckSwapRegister(btc.BridgeInstance, pairID); err != nil {
		return nil, err
	}
	swapInfo, err := btc.BridgeInstance.VerifyP2shTransaction(pairID, txidstr, *bindAddr, true)
	if !tokens.ShouldRegisterSwapForError(err) {
		return nil, newRPCError(-32099, "verify p2sh swapin failed! "+err.Error())
	}
	var memo string
	if err != nil {
		memo = err.Error()
	}
	swap := &mongodb.MgoSwap{
		PairID:    swapInfo.PairID,
		TxID:      txidstr,
		TxTo:      swapInfo.TxTo,
		TxType:    uint32(tokens.P2shSwapinTx),
		Bind:      *bindAddr,
		Status:    mongodb.GetStatusByTokenVerifyError(err),
		Timestamp: time.Now().Unix(),
		Memo:      memo,
	}
	err = mongodb.AddSwapin(swap)
	if err != nil {
		return nil, err
	}
	log.Info("[api] add p2sh swapin", "swap", swap)
	return &SuccessPostResult, nil
}

// GetLatestScanInfo api
func GetLatestScanInfo(isSrc bool) (*LatestScanInfo, error) {
	return mongodb.FindLatestScanInfo(isSrc)
}

// RegisterSwapPending register Swap for ETH like chain
func RegisterSwapPending(chain, txid string) (*PostResult, error) {
	if !params.MustRegisterAccount() {
		return &SuccessPostResult, nil
	}
	//chain = strings.ToLower(chain)
	txid = strings.ToLower(txid)
	ok := params.CheckChainSupport(chain)
	if !ok {
		supportErr := fmt.Sprintf("chain '%v' is not support want %v", chain, params.GetChainSupport())
		return nil, errors.New(supportErr)
	}
	ok = params.CheckTxID(txid)
	if !ok {
		return nil, errors.New("tx format error")
	}
	err := mongodb.AddRegisteredSwapPending(chain, txid)
	if err != nil {
		return nil, err
	}
	log.Info("[api] register swap pending", "chain", chain, "txid", txid)
	return &SuccessPostResult, nil
}

func BuildRegisterSwap(chain, txid string) error {
	//chain = strings.ToLower(chain)
	txid = strings.ToLower(txid)
	ok := params.CheckChainSupport(chain)
	if !ok {
		supportErr := fmt.Sprintf("chain '%v' is not support want %v", chain, params.GetChainSupport())
		return errors.New(supportErr)
	}
	ok = params.CheckTxID(txid)
	if !ok {
		return errors.New("tx format error")
	}
	post, err := mongodb.FindRegisterdSwapTxid(txid)
	if err == nil {
		return errors.New("already register")
	}
	err = eth.ParseTx(chain, txid)
	if err != nil {
		return err
	}
	err = mongodb.AddRegisteredSwapPending(chain, txid)
	if err != nil {
		return err
	}
	post, err = mongodb.FindRegisterdSwapTxid(txid)
	if err != nil {
		return err
	}
	worker.PostBridgeSwap(post)
	log.Info("[api] register swap pending", "chain", chain, "txid", txid)
	return nil
}

// RegisterSwapStatus register Swap for ETH like chain
func RegisterSwapStatus(txid string) (*SwapRegisterStatus, error) {
	if !params.MustRegisterAccount() {
		return nil, nil
	}
	txid = strings.ToLower(txid)
	ok := params.CheckTxID(txid)
	if !ok {
		return nil, errors.New("tx format error")
	}

	var result SwapRegisterStatus
	result.Txid = txid

	pStatus, err := mongodb.FindSwapPendingStatus(txid)
	if err == nil {
		var submit submitStatus
		if len(pStatus.Chain) != 0 {
			result.Chain = pStatus.Chain
		}
		submit.Status = mongodb.GetRegisterStatus(int(pStatus.Status))
		submit.Time = pStatus.Time
		result.Submit = &submit
	}
	rStatus, errR := mongodb.FindRegisteredSwapStatus(txid)
	if errR == nil {
		if len(rStatus.PairID) != 0 { // bridge
			var post postBridgeStatus
			post.Pairid = rStatus.PairID
			post.RpcMethod = rStatus.Method
			post.Status = mongodb.GetRegisterStatus(int(rStatus.Status))
			post.Time = rStatus.Time
			result.Register = &post
			if len(rStatus.Chain) != 0 {
				result.Chain = rStatus.Chain
			}
		} else {
			var post postRouterStatus
			post.LogIndex = fmt.Sprintf("%v", rStatus.LogIndex)
			post.RpcMethod = rStatus.Method
			post.Status = mongodb.GetRegisterStatus(int(rStatus.Status))
			post.Time = rStatus.Time
			result.Register = &post
			if rStatus.ChainID != 0 {
				result.Chain = fmt.Sprintf("%v", rStatus.ChainID)
			}
		}
	}
	log.Info("[api] register swap status", "txid", txid, "result", result)
	return &result, nil
}

// RegisterSwap register Swap for ETH like chain
func RegisterSwap(chain, method, pairid, txid, swapServer string) (*PostResult, error) {
	if !params.MustRegisterAccount() {
		return &SuccessPostResult, nil
	}
	chain = strings.ToLower(chain)
	//method = strings.ToLower(method)
	pairid = strings.ToLower(pairid)
	txid = strings.ToLower(txid)
	swapServer = strings.ToLower(swapServer)
	err := mongodb.AddRegisteredSwap(chain, method, pairid, txid, "0", "0", swapServer)
	if err != nil {
		return nil, err
	}
	log.Info("[api] register swap", "pairid", pairid, "method", method, "txid", txid, "swapServer", swapServer)
	return &SuccessPostResult, nil
}

// RegisterSwapRouter register Swap for ETH like chain
func RegisterSwapRouter(chain, method, chainid, txid, logIndex, swapServer string) (*PostResult, error) {
	if !params.MustRegisterAccount() {
		return &SuccessPostResult, nil
	}
	chain = strings.ToLower(chain)
	//method = strings.ToLower(method)
	chainid = strings.ToLower(chainid)
	txid = strings.ToLower(txid)
	logIndex = strings.ToLower(logIndex)
	swapServer = strings.ToLower(swapServer)
	err := mongodb.AddRegisteredSwap(chain, method, "", txid, chainid, logIndex, swapServer)
	if err != nil {
		return nil, err
	}
	log.Info("[api] register swap router", "chainid", chainid, "method", method, "txid", txid, "logIndex", logIndex, "swapServer", swapServer)
	return &SuccessPostResult, nil
}

// RegisterAddress register address for ETH like chain
func RegisterAddress(address string) (*PostResult, error) {
	if !params.MustRegisterAccount() {
		return &SuccessPostResult, nil
	}
	address = strings.ToLower(address)
	err := mongodb.AddRegisteredAddress(address)
	if err != nil {
		return nil, err
	}
	log.Info("[api] register address", "address", address)
	return &SuccessPostResult, nil
}

// GetRegisteredAddress get registered address
func GetRegisteredAddress(address string) (*RegisteredAddress, error) {
	address = strings.ToLower(address)
	return mongodb.FindRegisteredAddress(address)
}
