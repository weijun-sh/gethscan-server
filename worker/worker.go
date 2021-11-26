package worker

import (
	"time"

	"github.com/weijun-sh/gethscan-server/rpc/client"
	//"github.com/weijun-sh/gethscan-server/tokens/bridge"
)

const interval = 10 * time.Millisecond

// StartWork start swap server work
func StartWork(isServer bool) {
	if isServer {
		logWorker("worker", "start server worker")
	} else {
		logWorker("worker", "start oracle worker")
	}

	client.InitHTTPClient()
	StartPostJob()
	return
	//bridge.InitCrossChainBridge(isServer)

	//StartScanJob(isServer)
	//time.Sleep(interval)

	//StartUpdateLatestBlockHeightJob()
	//time.Sleep(interval)

	//if !isServer {
	//	StartAcceptSignJob()
	//	time.Sleep(interval)
	//	AddTokenPairDynamically()
	//	return
	//}

	//StartSwapJob()
	//time.Sleep(interval)

	//StartVerifyJob()
	//time.Sleep(interval)

	//StartStableJob()
	//time.Sleep(interval)

	//StartReplaceJob()
	//time.Sleep(interval)

	//StartPassBigValueJob()
	//time.Sleep(interval)

	//StartAggregateJob()
}
