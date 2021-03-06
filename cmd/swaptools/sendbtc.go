// nolint:dupl // keep it
package main

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/weijun-sh/gethscan-server/cmd/utils"
	"github.com/weijun-sh/gethscan-server/common"
	"github.com/weijun-sh/gethscan-server/log"
	"github.com/weijun-sh/gethscan-server/tokens"
	"github.com/weijun-sh/gethscan-server/tokens/btc"
	"github.com/weijun-sh/gethscan-server/tools"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/base58"
	"github.com/urfave/cli/v2"
)

var (
	// nolint:lll // allow long line of example
	sendBtcCommand = &cli.Command{
		Action:    sendBtc,
		Name:      "sendbtc",
		Usage:     "send btc",
		ArgsUsage: " ",
		Description: `
send btc command, sign tx with WIF or private key.

Example:

./swaptools sendbtc --gateway http://1.2.3.4:5555 --net testnet3 --wif ./wif.txt --from maiApsjjnceZ7Cx1UMj344JRU3R8A2Say6 --to mtc4xaZgJJZpN6BdoWk7pHFho1GTUnd5aP --value 10000 --to mfwanCuht2b4Lvb5XTds4Rvzy3jZ2ZWraL --value 20000 --memo "test send btc" --dryrun
`,
		Flags: []cli.Flag{
			utils.GatewayFlag,
			networkFlag,
			wifFileFlag,
			priKeyFileFlag,
			senderFlag,
			receiverSliceFlag,
			valueSliceFlag,
			memoFlag,
			relayFeePerKbFlag,
			dryRunFlag,
		},
	}
)

type btcTxSender struct {
	gateway       string
	netID         string
	wifFile       string
	priFile       string
	sender        string
	receivers     []string
	amounts       []int64
	memo          string
	relayFeePerKb int64
	dryRun        bool
}

var (
	btcBridge *btc.Bridge
	btcSender = &btcTxSender{}
)

func (bts *btcTxSender) initArgs(ctx *cli.Context) {
	bts.gateway = ctx.String(utils.GatewayFlag.Name)
	bts.netID = ctx.String(networkFlag.Name)
	bts.wifFile = ctx.String(wifFileFlag.Name)
	bts.priFile = ctx.String(priKeyFileFlag.Name)
	bts.sender = ctx.String(senderFlag.Name)
	bts.receivers = ctx.StringSlice(receiverSliceFlag.Name)
	bts.amounts = ctx.Int64Slice(valueSliceFlag.Name)
	bts.memo = ctx.String(memoFlag.Name)
	bts.relayFeePerKb = ctx.Int64(relayFeePerKbFlag.Name)
	bts.dryRun = ctx.Bool(dryRunFlag.Name)

	if bts.netID == "" {
		log.Fatal("must specify '-net' flag")
	}
	if bts.wifFile == "" && bts.priFile == "" {
		log.Fatal("must specify '-wif' or '-pri' flag")
	}
	if bts.sender == "" {
		log.Fatal("must specify '-from' flag")
	}
	if len(bts.receivers) == 0 {
		log.Fatal("must specify '-to' flag")
	}
	if len(bts.amounts) == 0 {
		log.Fatal("must specify '-value' flag")
	}
	if len(bts.receivers) != len(bts.amounts) {
		log.Fatal("count of receivers and values are not equal")
	}
}

func sendBtc(ctx *cli.Context) error {
	utils.SetLogger(ctx)
	btcSender.initArgs(ctx)

	btcSender.initBridge()

	wifStr := btcSender.loadWIFForAddress()

	rawTx, err := btcBridge.BuildTransaction(btcSender.sender, btcSender.receivers, btcSender.amounts, btcSender.memo, btcSender.relayFeePerKb)
	if err != nil {
		log.Fatal("BuildRawTransaction error", "err", err)
	}

	signedTx, txHash, err := btcBridge.SignTransactionWithWIF(rawTx, wifStr)
	if err != nil {
		log.Fatal("SignTransaction failed", "err", err)
	}
	log.Info("SignTransaction success", "txHash", txHash)

	fmt.Println(btc.AuthoredTxToString(signedTx, true))

	if !btcSender.dryRun {
		_, err = btcBridge.SendTransaction(signedTx)
		if err != nil {
			log.Error("SendTransaction failed", "err", err)
		}
	} else {
		log.Info("------------ dry run, does not sendtx -------------")
	}
	return nil
}

func (bts *btcTxSender) initBridge() {
	btcBridge = btc.NewCrossChainBridge(true)
	btcBridge.ChainConfig = &tokens.ChainConfig{
		BlockChain: "Bitcoin",
		NetID:      bts.netID,
	}
	btcBridge.GatewayConfig = &tokens.GatewayConfig{
		APIAddress: []string{bts.gateway},
	}
}

func (bts *btcTxSender) loadWIFForAddress() string {
	var wifStr string
	if bts.wifFile != "" {
		wifdata, err := tools.SafeReadFile(bts.wifFile)
		if err != nil {
			log.Fatal("Read WIF file failed", "err", err)
		}
		wifStr = strings.TrimSpace(string(wifdata))
	} else {
		pridata, err := tools.SafeReadFile(bts.priFile)
		if err != nil {
			log.Fatal("Read private key file failed", "err", err)
		}
		priKey := strings.TrimSpace(string(pridata))
		var pribs []byte
		if common.IsHex(priKey) {
			pribs, err = hex.DecodeString(priKey)
			if err != nil {
				log.Fatal("failed to decode hex private key string")
			}
		} else {
			pribs, _, err = base58.CheckDecode(priKey)
			if err != nil {
				pribs = base58.Decode(priKey)
			}
		}
		pri, _ := btcec.PrivKeyFromBytes(btcec.S256(), pribs)
		wif, err := btcutil.NewWIF(pri, btcBridge.GetChainParams(), true)
		if err != nil {
			log.Fatal("failed to parse private key")
		}
		wifStr = wif.String()
	}
	wif, err := btcutil.DecodeWIF(wifStr)
	if err != nil {
		log.Fatal("failed to decode WIF to verify")
	}
	pkdata := wif.SerializePubKey()
	pkaddr, _ := btcBridge.NewAddressPubKeyHash(pkdata)
	if pkaddr.EncodeAddress() != bts.sender {
		log.Fatal("address mismatch", "decoded", pkaddr.EncodeAddress(), "from", bts.sender)
	}
	return wifStr
}
