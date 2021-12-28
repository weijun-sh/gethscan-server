package worker

import (
	"time"

	"github.com/weijun-sh/gethscan-server/cmd/utils"
	"github.com/weijun-sh/gethscan-server/tokens"
	"github.com/weijun-sh/gethscan-server/tokens/eth"
	"github.com/weijun-sh/gethscan-server/tokens/btc"
)

// StartScanJob scan job
func StartScanJob(isServer bool) {
	srcChainCfg := tokens.SrcBridge.GetChainConfig()
	if srcChainCfg.EnableScan && btc.BridgeInstance != nil {
		go btc.BridgeInstance.StartChainTransactionScanJob()
		if srcChainCfg.EnableScanPool {
			go btc.BridgeInstance.StartPoolTransactionScanJob()
		}
		go btc.BridgeInstance.StartSwapHistoryScanJob()
	}
}

func StartParseChainTx() {
	eth.InitCrossChain()
	go loopParseChainTx()
}

func loopParseChainTx() {
	for loop := 1; ; loop++ {
		if utils.IsCleanuping() {
			return
		}
		eth.FindSwapPendingAndRegister()
		time.Sleep(postInterval)
	}
}

