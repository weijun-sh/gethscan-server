package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weijun-sh/gethscan-server/log"
	"github.com/weijun-sh/gethscan-server/mongodb"
	"github.com/weijun-sh/gethscan-server/rpc/client"
	"github.com/weijun-sh/gethscan-server/params"
	"github.com/weijun-sh/gethscan-server/tokens"
)

var (
	rpcRetryCount   = 3
	rpcInterval     = 1 * time.Second
	postInterval    = 1 * time.Second
	cachedSwapPosts *Ring
)

const (
	postSwapSuccessResult          = "success"
	bridgeSwapExistKeywords        = "mgoError: Item is duplicate"
	routerSwapExistResult          = "already registered"
	routerSwapExistResultTmp       = "alreday registered"
	httpTimeoutKeywords            = "Client.Timeout exceeded while awaiting headers"
	errConnectionRefused           = "connect: connection refused"
	errMaximumRequestLimit         = "You have reached maximum request limit"
	rpcQueryErrKeywords            = "rpc query error"
	errDepositLogNotFountorRemoved = "return error: json-rpc error -32099, verify swap failed! deposit log not found or removed"
	swapIsClosedResult             = "swap is closed"
	swapTradeNotSupport            = "swap trade not support"
	txWithWrongContract            = "tx with wrong contract"
)

// StartAggregateJob aggregate job
func StartPostJob() {
	mongodb.MgoWaitGroup.Add(1)
	cachedSwapPosts = NewRing(100)
	go loopSwapRegister()
}

func loopSwapRegister() {
	log.Info("start SwapRegister loop job")
	offset := 0
	MaxParseRegisteredLimit := params.GetMaxParseRegisteredLimit()
	if MaxParseRegisteredLimit < 10 {
		MaxParseRegisteredLimit = 10
	}
	fmt.Printf("MaxParseRegisteredLimit : %v\n", MaxParseRegisteredLimit)
	for {
		sp, err := mongodb.FindRegisterdSwap("", offset, MaxParseRegisteredLimit)
		lenPending := len(sp)
		if err != nil || lenPending == 0 {
			offset = 0
			time.Sleep(2 * time.Second)
			continue
		}
		log.Info("loopSwapRegister", "swap", sp, "len", lenPending)
		for _, p := range sp {
			PostBridgeSwap(p)
		}
                offset += MaxParseRegisteredLimit
                if lenPending < MaxParseRegisteredLimit {
                        offset = 0
                        time.Sleep(1 * time.Second)
                }
                time.Sleep(1 * time.Second)
	}
}

func PostBridgeSwap(p *mongodb.MgoRegisteredSwap) (error, error) {
	ok, err := postBridgeSwap(p)
	if ok == nil {
		if err == nil {
			log.Info("post Swap success", "Key", p.Key, "chainID", p.ChainID, "pairID", p.PairID, "method", p.Method, "rpc", p.SwapServer)
			mongodb.UpdateRegisteredSwapStatusSuccess(p.Key)
		} else {
			log.Info("post Swap finished", "Key", p.Key, "chainID", p.ChainID, "pairID", p.PairID, "method", p.Method, "rpc", p.SwapServer, "err", err)
			mongodb.UpdateRegisteredSwapStatus(p.Key, err.Error())
		}
		return ok, err
	} else {
		//mongodb.UpdateRegisteredSwapStatusFailed(p.Key)
		log.Warn("post Swap fail", "Key", p.Key, "chainID", p.ChainID, "pairID", p.PairID, "method", p.Method, "rpc", p.SwapServer, "err", ok)
	}
	return err, nil
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

func postBridgeSwap(post *mongodb.MgoRegisteredSwap) (error, error) {
	swap := &swapPost{
		txid:       post.Key,
		pairID:     post.PairID,
		rpcMethod:  post.Method,
		chainID:    fmt.Sprintf("%v", post.ChainID),
		logIndex:   fmt.Sprintf("%v", post.LogIndex),
		swapServer: post.SwapServer,
	}
	return postSwapPost(swap)
}

func postSwapPost(swap *swapPost) (error, error) {
	var needCached bool
	var errPending error = errors.New("Post err")
	for i := 0; i < rpcRetryCount; i++ {
		ok, err := rpcPost(swap)
		if ok == nil {
			return nil, err
		}
		log.Warn("postSwapPost", "err", err)
		if errors.Is(err, tokens.ErrTxNotFound) ||
			strings.Contains(err.Error(), httpTimeoutKeywords) ||
			strings.Contains(err.Error(), errConnectionRefused) ||
			strings.Contains(err.Error(), errMaximumRequestLimit) {
			needCached = true
		} else {
			errPending = nil
		}
		time.Sleep(rpcInterval)
	}
	if needCached {
		log.Warn("cache swap", "swap", swap)
		cachedSwapPosts.Add(swap)
	}
	return errPending, nil
}

func rpcPost(swap *swapPost) (error, error) {
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
		return nil, errors.New("wrong swap post item")
	}

	timeout := 300
	reqID := 666
	var result interface{}
	err := client.RPCPostWithTimeoutAndID(&result, timeout, reqID, swap.swapServer, swap.rpcMethod, args)

	if err != nil {
		errmsg := fmt.Sprintf("%v", err)
		if strings.Contains(errmsg, ", ") {
			slice := strings.Split(errmsg, ", ")
			errmsg = slice[1]
		}
		err = errors.New(errmsg)
		ok, err1 := checkSwapPostError(err, args)
		if ok == nil {
			return nil, err1
		}
		if isRouterSwap {
			log.Warn("post router swap failed", "swap", args, "server", swap.swapServer, "err", err)
			return err, nil
		}
		if strings.Contains(err.Error(), bridgeSwapExistKeywords) {
			log.Info("post bridge swap already exist", "swap", args)
			return nil, err
		} else {
			log.Warn("post bridge swap failed", "swap", args, "server", swap.swapServer, "err", err)
			return err, nil
		}
	}

	if !isRouterSwap {
		log.Info("post bridge swap success", "swap", args)
		return nil, nil
	}

	var status string
	if res, ok := result.(map[string]interface{}); ok {
		status, _ = res[swap.logIndex].(string)
	}
	if status == "" {
		err = errors.New("post router swap unmarshal result failed")
		log.Error(err.Error(), "swap", args, "server", swap.swapServer, "result", result)
		var resultMap map[string]interface{}
		b, _ := json.Marshal(&result)
		json.Unmarshal(b, &resultMap)
		for _, value := range resultMap {
			if strings.Contains(value.(string), routerSwapExistResult) ||
				strings.Contains(value.(string), routerSwapExistResultTmp) {
				log.Info("post router swap already exist", "swap", args)
				return nil, nil
			}
		}
		return err, nil
	}
	return checkRouterStatus(status, args)
}

func checkSwapPostError(err error, args interface{}) (error, error) {
	if strings.Contains(err.Error(), routerSwapExistResult) ||
		strings.Contains(err.Error(), routerSwapExistResultTmp) {
		log.Info("post swap already exist", "swap", args)
		return nil, err
	}
	if strings.Contains(err.Error(), swapIsClosedResult) {
		log.Info("post router swap failed, swap is closed", "swap", args)
		return nil, err
	}
	if strings.Contains(err.Error(), swapTradeNotSupport) {
		log.Info("post router swap failed, swap trade not support", "swap", args)
		return nil, err
	}
	return err, nil
}

func checkRouterStatus(status string, args interface{}) (error, error) {
	if strings.Contains(status, postSwapSuccessResult) {
		log.Info("post router swap success", "swap", args)
		return nil, nil
	}
	if strings.Contains(status, routerSwapExistResult) ||
		strings.Contains(status, routerSwapExistResultTmp) {
		log.Info("post router swap already exist", "swap", args)
		return nil, errors.New(routerSwapExistResult)
	}
	if strings.Contains(status, txWithWrongContract) {
		log.Info("post router swap failed, tx with wrong contract", "swap", args)
		return nil, errors.New(txWithWrongContract)
	}
	err := errors.New(status)
	log.Info("post router swap failed", "swap", args, "err", err)
	return err, nil
}
