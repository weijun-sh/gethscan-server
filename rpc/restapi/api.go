// Package restapi provides RESTful RPC service.
package restapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/weijun-sh/gethscan-server/common"
	"github.com/weijun-sh/gethscan-server/internal/swapapi"
	"github.com/weijun-sh/gethscan-server/log"
	"github.com/weijun-sh/gethscan-server/params"
	"github.com/gorilla/mux"
)

func writeResponse(w http.ResponseWriter, resp interface{}, err error) {
	if err != nil {
		writeErrResponse(w, err)
		return
	}
	jsonData, err := json.Marshal(resp)
	if err != nil {
		writeErrResponse(w, err)
		return
	}
	writeJSONResponse(w, jsonData)
}

func writeJSONResponse(w http.ResponseWriter, jsonData []byte) {
	// Note: must set header before write header
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(jsonData)
	if err != nil {
		log.Warn("write response error", "data", common.ToHex(jsonData), "err", err)
	}
}

func writeErrResponse(w http.ResponseWriter, err error) {
	// Note: must set header before write header
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, err.Error())
}

// VersionInfoHandler handler
func VersionInfoHandler(w http.ResponseWriter, r *http.Request) {
	version := params.VersionWithMeta
	writeResponse(w, version, nil)
}

// ServerInfoHandler handler
func ServerInfoHandler(w http.ResponseWriter, r *http.Request) {
	res, err := swapapi.GetServerInfo()
	writeResponse(w, res, err)
}

// TokenPairInfoHandler handler
func TokenPairInfoHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	pairID := vars["pairid"]
	res, err := swapapi.GetTokenPairInfo(pairID)
	writeResponse(w, res, err)
}

// TokenPairsInfoHandler handler
func TokenPairsInfoHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	pairIDs := vars["pairids"]
	res, err := swapapi.GetTokenPairsInfo(pairIDs)
	writeResponse(w, res, err)
}

// NonceInfoHandler handler
func NonceInfoHandler(w http.ResponseWriter, r *http.Request) {
	res, err := swapapi.GetNonceInfo()
	writeResponse(w, res, err)
}

// StatisticsHandler handler
func StatisticsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	pairID := vars["pairid"]
	res, err := swapapi.GetSwapStatistics(pairID)
	writeResponse(w, res, err)
}

func getBindParam(r *http.Request) string {
	vals := r.URL.Query()
	bindVals, exist := vals["bind"]
	if exist {
		return bindVals[0]
	}
	return ""
}

// GetRawSwapinHandler handler
func GetRawSwapinHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	pairID := vars["pairid"]
	bind := getBindParam(r)
	res, err := swapapi.GetRawSwapin(&txid, &pairID, &bind)
	writeResponse(w, res, err)
}

// GetRawSwapinResultHandler handler
func GetRawSwapinResultHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	pairID := vars["pairid"]
	bind := getBindParam(r)
	res, err := swapapi.GetRawSwapinResult(&txid, &pairID, &bind)
	writeResponse(w, res, err)
}

// GetSwapinHandler handler
func GetSwapinHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	pairID := vars["pairid"]
	bind := getBindParam(r)
	res, err := swapapi.GetSwapin(&txid, &pairID, &bind)
	writeResponse(w, res, err)
}

// GetRawSwapoutHandler handler
func GetRawSwapoutHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	pairID := vars["pairid"]
	bind := getBindParam(r)
	res, err := swapapi.GetRawSwapout(&txid, &pairID, &bind)
	writeResponse(w, res, err)
}

// GetRawSwapoutResultHandler handler
func GetRawSwapoutResultHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	pairID := vars["pairid"]
	bind := getBindParam(r)
	res, err := swapapi.GetRawSwapoutResult(&txid, &pairID, &bind)
	writeResponse(w, res, err)
}

// GetSwapoutHandler handler
func GetSwapoutHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	pairID := vars["pairid"]
	bind := getBindParam(r)
	res, err := swapapi.GetSwapout(&txid, &pairID, &bind)
	writeResponse(w, res, err)
}

type historyParams struct {
	address string
	pairID  string
	offset  int
	limit   int
	status  string
}

func getHistoryParams(r *http.Request) (p *historyParams, err error) {
	vars := mux.Vars(r)
	vals := r.URL.Query()

	p = &historyParams{}

	p.address = vars["address"]
	p.pairID = vars["pairid"]

	offsetStr, exist := vals["offset"]
	if exist {
		p.offset, err = common.GetIntFromStr(offsetStr[0])
		if err != nil {
			return p, err
		}
	}

	limitStr, exist := vals["limit"]
	if exist {
		p.limit, err = common.GetIntFromStr(limitStr[0])
		if err != nil {
			return p, err
		}
	}

	statusStr, exist := vals["status"]
	if exist {
		p.status = statusStr[0]
	}

	return p, nil
}

// SwapinHistoryHandler handler
func SwapinHistoryHandler(w http.ResponseWriter, r *http.Request) {
	p, err := getHistoryParams(r)
	if err != nil {
		writeResponse(w, nil, err)
	} else {
		res, err := swapapi.GetSwapinHistory(p.address, p.pairID, p.offset, p.limit, p.status)
		writeResponse(w, res, err)
	}
}

// SwapoutHistoryHandler handler
func SwapoutHistoryHandler(w http.ResponseWriter, r *http.Request) {
	p, err := getHistoryParams(r)
	if err != nil {
		writeResponse(w, nil, err)
	} else {
		res, err := swapapi.GetSwapoutHistory(p.address, p.pairID, p.offset, p.limit, p.status)
		writeResponse(w, res, err)
	}
}

// RegisterSwapPendingHandler handler
func RegisterSwapPendingHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chain := vars["chain"]
	txid := vars["txid"]
	res, err := swapapi.RegisterSwapPending(chain, txid)
	writeResponse(w, res, err)
}

// RegisterSwapHandler handler
func RegisterSwapHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	pairid := vars["pairid"]
	txid := vars["txid"]
	method := vars["method"]
	swapServer := vars["swapserver"]
	//chain := vars["chain"]
	res, err := swapapi.RegisterSwap("", method, pairid, txid, swapServer)
	writeResponse(w, res, err)
}

// RegisterSwapRouterHandler handler
func RegisterSwapRouterHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("RegisterSwapRouterHandler, r: %v\n", r)
	vars := mux.Vars(r)
	chainid := vars["chainid"]
	txid := vars["txid"]
	logindex := vars["logindex"]
	method := vars["method"]
	swapServer := vars["swapserver"]
	//chain := vars["chain"]
	res, err := swapapi.RegisterSwapRouter("", method, chainid, txid, logindex, swapServer)
	writeResponse(w, res, err)
}

// PostSwapinHandler handler
func PostSwapinHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	pairID := vars["pairid"]
	res, err := swapapi.Swapin(&txid, &pairID)
	writeResponse(w, res, err)
}

// RetrySwapinHandler handler
func RetrySwapinHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	pairID := vars["pairid"]
	res, err := swapapi.RetrySwapin(&txid, &pairID)
	writeResponse(w, res, err)
}

// PostP2shSwapinHandler handler
func PostP2shSwapinHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	bind := vars["bind"]
	res, err := swapapi.P2shSwapin(&txid, &bind)
	writeResponse(w, res, err)
}

// PostSwapoutHandler handler
func PostSwapoutHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]
	pairID := vars["pairid"]
	res, err := swapapi.Swapout(&txid, &pairID)
	writeResponse(w, res, err)
}

// RegisterP2shAddress handler
func RegisterP2shAddress(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]
	res, err := swapapi.RegisterP2shAddress(address)
	writeResponse(w, res, err)
}

// GetP2shAddressInfo handler
func GetP2shAddressInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]
	res, err := swapapi.GetP2shAddressInfo(address)
	writeResponse(w, res, err)
}

// RegisterAddress handler
func RegisterAddress(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]
	res, err := swapapi.RegisterAddress(address)
	writeResponse(w, res, err)
}

// GetRegisteredAddress handler
func GetRegisteredAddress(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]
	res, err := swapapi.GetRegisteredAddress(address)
	writeResponse(w, res, err)
}
