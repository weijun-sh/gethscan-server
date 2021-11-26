package worker

import (
	"github.com/weijun-sh/gethscan-server/tokens"
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
