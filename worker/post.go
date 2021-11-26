package worker

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weijun-sh/gethscan-server/cmd/utils"
	"github.com/weijun-sh/gethscan-server/mongodb"
	"github.com/weijun-sh/gethscan-server/log"
	"github.com/weijun-sh/gethscan-server/tokens"
	"github.com/weijun-sh/gethscan-server/rpc/client"
)

var (
	rpcRetryCount = 3
	rpcInterval = 1 * time.Second
	postInterval = 5 * time.Second
	cachedSwapPosts *Ring
)

const (
	postSwapSuccessResult   = "success"
	bridgeSwapExistKeywords = "mgoError: Item is duplicate"
	routerSwapExistResult   = "already registered"
	routerSwapExistResultTmp   = "alreday registered"
	httpTimeoutKeywords     = "Client.Timeout exceeded while awaiting headers"
	errConnectionRefused    = "connect: connection refused"
	rpcQueryErrKeywords     = "rpc query error"
	errDepositLogNotFountorRemoved = "return error: json-rpc error -32099, verify swap failed! deposit log not found or removed"
)

// StartAggregateJob aggregate job
func StartPostJob() {
	mongodb.MgoWaitGroup.Add(1)
	cachedSwapPosts = NewRing(100)
	go loopDoPostJob()
}

func loopDoPostJob() {
	defer mongodb.MgoWaitGroup.Done()
	for loop := 1; ; loop++ {
		if utils.IsCleanuping() {
			return
		}
		findSwapAndPost()
		time.Sleep(postInterval)
	}
}

func findSwapAndPost() {
        post, err := mongodb.FindRegisterdSwap(0, 10)
        if err != nil || len(post) == 0 {
               time.Sleep(3 * postInterval)
               return
	}
	for _, p := range post {
		ok := postBridgeSwap(p.Method, p.PairID, p.Key, p.SwapServer)
		if ok == nil {
			//updateBridgeSwap(p.Method, p.PairID, p.Key, p.SwapServer)
			mongodb.AddSwapPost(p.Method, p.PairID, p.Key, p.SwapServer)
		}
	}
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

func postBridgeSwap(method, pairID, txid, swapServer string) error {
	//rpcMethod = "swap.Swapin"
	//rpcMethod = "swap.Swapout"
	log.Info("postBridgeSwap", "txid", txid, "pairID", pairID, "method", method, "rpc", swapServer)
	swap := &swapPost{
		txid:       txid,
		pairID:     pairID,
		rpcMethod:  method,
		swapServer: swapServer,
	}
	return postSwapPost(swap)
}

func postSwapPost(swap *swapPost) error {
	var needCached bool
	//var needPending bool
	for i := 0; i < rpcRetryCount; i++ {
		err := rpcPost(swap)
		if err == nil {
			return nil
		}
		if errors.Is(err, tokens.ErrTxNotFound) ||
			strings.Contains(err.Error(), httpTimeoutKeywords) ||
			strings.Contains(err.Error(), errConnectionRefused) {
			needCached = true
			//needPending = true
		}
		time.Sleep(rpcInterval)
	}
	if needCached {
		log.Warn("cache swap", "swap", swap)
		cachedSwapPosts.Add(swap)
	}
	return errors.New("Post err")
       //if needPending {
       //        if mongodbEnable {
       //                //insert mongo post pending
       //                addMongodbSwapPendingPost(swap)
       //        }
       //}
       //if !needCached && !needPending {
       //        if mongodbEnable {
       //                //insert mongo post
       //                addMongodbSwapPost(swap)
       //        }
       //}
}

func rpcPost(swap *swapPost) error {
	var isRouterSwap bool
	var args interface{}
	if swap.pairID != "" {
		args = map[string]interface{}{
			"txid":   swap.txid,
			"pairid": swap.pairID,
		}
	} else if swap.logIndex != "" {
		isRouterSwap = true
		args = map[string]string{
			"chainid":  swap.chainID,
			"txid":     swap.txid,
			"logindex": swap.logIndex,
		}
	} else {
		return fmt.Errorf("wrong swap post item %v", swap)
	}

	timeout := 300
	reqID := 666
	var result interface{}
	err := client.RPCPostWithTimeoutAndID(&result, timeout, reqID, swap.swapServer, swap.rpcMethod, args)

	if err != nil {
		if isRouterSwap {
			log.Warn("post router swap failed", "swap", args, "server", swap.swapServer, "err", err)
			return err
		}
		if strings.Contains(err.Error(), bridgeSwapExistKeywords) {
			err = nil // ignore this kind of error
			log.Info("post bridge swap already exist", "swap", args)
		} else {
			log.Warn("post bridge swap failed", "swap", args, "server", swap.swapServer, "err", err)
		}
		return err
	}

	if !isRouterSwap {
		log.Info("post bridge swap success", "swap", args)
		return nil
	}

	var status string
	if res, ok := result.(map[string]interface{}); ok {
		status, _ = res[swap.logIndex].(string)
	}
	if status == "" {
		err = errors.New("post router swap unmarshal result failed")
		log.Error(err.Error(), "swap", args, "server", swap.swapServer, "result", result)
		return err
	}
	switch status {
	case postSwapSuccessResult:
		log.Info("post router swap success", "swap", args)
	case routerSwapExistResult, routerSwapExistResultTmp:
		log.Info("post router swap already exist", "swap", args)
	default:
		err = errors.New(status)
		log.Info("post router swap failed", "swap", args, "server", swap.swapServer, "err", err)
	}
	return err
}

