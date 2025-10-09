package api

import (
	"bytes"
	"encoding/json"
	"go.uber.org/zap"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/shuliakovsky/mezzium/pkg/registry"
	"github.com/shuliakovsky/mezzium/pkg/secrets"
)

type Public struct {
	Reg    *registry.Registry
	Logger *zap.Logger
}

func NewPublic(reg *registry.Registry, logger *zap.Logger) *Public {
	return &Public{Reg: reg, Logger: logger}
}

func (p *Public) NetworkFees(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_network_fees", r.Method, r.URL.Path, nil)

	nodes := p.Reg.Best("eth")
	if len(nodes) == 0 {
		http.Error(w, "no healthy ETH nodes", http.StatusServiceUnavailable)
		return
	}
	target := nodes[0]
	type rpcReq struct {
		Jsonrpc string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params,omitempty"`
		ID      int         `json:"id"`
	}
	req1, _ := json.Marshal(rpcReq{Jsonrpc: "2.0", Method: "eth_feeHistory", Params: []any{"0x1", "latest", []any{}}, ID: 1})
	req2, _ := json.Marshal(rpcReq{Jsonrpc: "2.0", Method: "eth_maxPriorityFeePerGas", ID: 2})

	client := &http.Client{}
	r1, _ := http.NewRequest("POST", target.URL, bytes.NewReader(req1))
	r2, _ := http.NewRequest("POST", target.URL, bytes.NewReader(req2))
	for k, v := range target.Headers {
		r1.Header.Set(k, v)
		r2.Header.Set(k, v)
	}
	r1.Header.Set("content-type", "application/json")
	r2.Header.Set("content-type", "application/json")

	type feeHistory struct {
		Result struct {
			BaseFeePerGas []string `json:"baseFeePerGas"`
		} `json:"result"`
	}
	type maxPrio struct {
		Result string `json:"result"`
	}
	var fh feeHistory
	var mp maxPrio

	resp1, err1 := client.Do(r1)
	if err1 != nil || resp1.StatusCode/100 != 2 {
		http.Error(w, "feeHistory failed", http.StatusBadGateway)
		return
	}
	defer resp1.Body.Close()
	_ = json.NewDecoder(resp1.Body).Decode(&fh)

	resp2, err2 := client.Do(r2)
	if err2 != nil || resp2.StatusCode/100 != 2 {
		http.Error(w, "maxPriority failed", http.StatusBadGateway)
		return
	}
	defer resp2.Body.Close()
	_ = json.NewDecoder(resp2.Body).Decode(&mp)

	respBody, _ := json.Marshal(map[string]any{
		"baseFee":        first(fh.Result.BaseFeePerGas),
		"maxPriorityFee": mp.Result,
	})
	w.Header().Set("content-type", "application/json")
	w.Write(respBody)
	LogResponse(p.Logger, "public_network_fees", http.StatusOK, respBody, start)
}

func (p *Public) ActiveNodes(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_active_nodes", r.Method, r.URL.Path, nil)
	if r.Method == http.MethodPost {
		type liteNode struct {
			URL      string `json:"url"`
			Priority int    `json:"priority"`
		}
		out := make(map[string][]liteNode)
		for name, st := range p.Reg.All() {
			if len(st.Best) == 0 {
				out[name] = []liteNode{}
				continue
			}
			arr := make([]liteNode, 0, len(st.Best))
			for _, n := range st.Best {
				arr = append(arr, liteNode{
					URL:      secrets.RedactString(n.URL),
					Priority: n.Priority,
				})
			}
			out[name] = arr
		}
		b, _ := json.Marshal(out)
		w.Header().Set("content-type", "application/json")
		w.Write(b)
		LogResponse(p.Logger, "public_active_nodes", http.StatusOK, b, start)
		return
	}

	all := p.Reg.All()
	resp := map[string]any{}
	for name, st := range all {
		var arr []map[string]any
		for _, n := range st.Best {
			arr = append(arr, map[string]any{
				"url":      secrets.RedactString(n.URL),
				"priority": n.Priority,
				"alive":    n.Alive,
				"ping":     n.Ping,
			})
		}
		resp[name] = arr
	}
	respBytes, _ := json.Marshal(resp)
	w.Header().Set("content-type", "application/json")
	w.Write(respBytes)
	LogResponse(p.Logger, "public_active_nodes", http.StatusOK, respBytes, start)
}

// GET /proxy/btc/fees → Tatum
func (p *Public) BTCFees(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_btc_fees", r.Method, r.URL.Path, nil)
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	forwardExternalAPI(w, "https://api.tatum.io/v3/blockchain/fee/BTC", "TATUM_API_KEY")
	LogResponse(p.Logger, "public_btc_fees", http.StatusOK, nil, start)
}

// GET /proxy/eth/fee → Tatum
func (p *Public) EthFee(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_eth_fee", r.Method, r.URL.Path, nil)
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	forwardExternalAPI(w, "https://api.tatum.io/v3/blockchain/fee/ETH", "TATUM_API_KEY")
	LogResponse(p.Logger, "public_eth_fee", http.StatusOK, nil, start)
}

// GET /proxy/eth/maxPriorityFee
func (p *Public) EthMaxPriorityFee(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_eth_max_priority_fee", r.Method, r.URL.Path, nil)

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	nodes := p.Reg.Best("eth")
	if len(nodes) == 0 {
		http.Error(w, "no healthy ETH nodes", http.StatusServiceUnavailable)
		return
	}
	target := nodes[0]
	payload := `{"jsonrpc":"2.0","id":1,"method":"eth_maxPriorityFeePerGas","params":[]}`
	req, _ := http.NewRequest(http.MethodPost, target.URL, strings.NewReader(payload))
	for k, v := range target.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "rpc call failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
	LogResponse(p.Logger, "public_eth_max_priority_fee", resp.StatusCode, respBody, start)
}

// GET /proxy/nft/get-all-nfts/{address}
func (p *Public) NFTGetAllNFTs(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_nft_get_all", r.Method, r.URL.Path, nil)

	const prefix = "/proxy/nft/get-all-nfts/"
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	address := strings.TrimPrefix(r.URL.Path, prefix)
	if address == "" {
		http.Error(w, "address required", http.StatusBadRequest)
		return
	}
	apiKey := os.Getenv("ALCHEMY_API_KEY")
	url := "https://eth-mainnet.g.alchemy.com/nft/v3/" + apiKey +
		"/getNFTsForOwner?owner=" + address + "&withMetadata=true&pageSize=100"
	forwardExternalGET(w, url, nil)
	LogResponse(p.Logger, "public_nft_get_all", http.StatusOK, nil, start)
}

// GET /proxy/nft/get-nft-metadata/{contract}/{tokenId}
func (p *Public) NFTGetNFTMetadata(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_nft_get_metadata", r.Method, r.URL.Path, nil)

	const prefix = "/proxy/nft/get-nft-metadata/"
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		http.Error(w, "contractAddress and tokenId required", http.StatusBadRequest)
		return
	}
	contract, tokenId := parts[0], parts[1]
	apiKey := os.Getenv("ALCHEMY_API_KEY")
	url := "https://eth-mainnet.g.alchemy.com/nft/v3/" + apiKey +
		"/getNFTMetadata?contractAddress=" + contract + "&tokenId=" + tokenId + "&refreshCache=false"
	forwardExternalGET(w, url, nil)
	LogResponse(p.Logger, "public_nft_get_metadata", http.StatusOK, nil, start)
}

// POST /proxy/eth/estimateGas
func (p *Public) EthEstimateGas(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	start := LogRequest(p.Logger, "public_eth_estimate_gas", r.Method, r.URL.Path, body)

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	nodes := p.Reg.Best("eth")
	if len(nodes) == 0 {
		http.Error(w, "no healthy ETH nodes", http.StatusServiceUnavailable)
		return
	}

	// create JSON-RPC request
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_estimateGas",
		"params":  []any{},
	}
	if len(body) > 0 {
		// trying parse as params
		var params any
		if err := json.Unmarshal(body, &params); err == nil {
			payload["params"] = params
		} else {
			// fallback — as object
			payload["params"] = []any{json.RawMessage(body)}
		}
	}

	b, _ := json.Marshal(payload)

	// Send to the first healthy ETH node.
	tatumURL := "https://api.tatum.io/v3/blockchain/node/ethereum-mainnet"
	req, _ := http.NewRequest(http.MethodPost, tatumURL, bytes.NewReader(b))
	req.Header.Set("content-type", "application/json")
	if k := os.Getenv("TATUM_API_KEY"); k != "" {
		req.Header.Set("x-api-key", k)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "rpc call failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)

	LogResponse(p.Logger, "public_eth_estimate_gas", resp.StatusCode, respBody, start)
}

// first returns the first element of the slice, or an empty string if the slice is empty.
func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// forwardExternalAPI performs a GET request to an external REST API using the x-api-key from the environment variable.
func forwardExternalAPI(w http.ResponseWriter, url, apiKeyEnv string) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if k := os.Getenv(apiKeyEnv); k != "" {
		req.Header.Set("x-api-key", k)
	}
	req.Header.Set("accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "external api failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// forwardExternalGET performs a GET request to an external REST API with arbitrary headers.
func forwardExternalGET(w http.ResponseWriter, url string, headers map[string]string) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "external api failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// GET /proxy/btc/balance/{address} → Tatum
func (p *Public) BTCBalance(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_btc_balance", r.Method, r.URL.Path, nil)
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	const prefix = "/proxy/btc/balance/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	addr := strings.TrimPrefix(r.URL.Path, prefix)
	if addr == "" {
		http.Error(w, "address required", http.StatusBadRequest)
		return
	}
	url := "https://api.tatum.io/v3/bitcoin/address/balance/" + addr
	forwardExternalAPI(w, url, "TATUM_API_KEY")
	LogResponse(p.Logger, "public_btc_balance", http.StatusOK, nil, start)
}
