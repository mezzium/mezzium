package api

import (
	"bytes"
	"encoding/json"
	"go.uber.org/zap"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/oracle"
	"github.com/shuliakovsky/mezzium/pkg/registry"
	"github.com/shuliakovsky/mezzium/pkg/secrets"
)

var evmThrottler = NewThrottler()

type Public struct {
	Reg    *registry.Registry
	Logger *zap.Logger
}

func NewPublic(reg *registry.Registry, logger *zap.Logger) *Public {
	return &Public{Reg: reg, Logger: logger}
}

func (p *Public) NetworkFees(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_network_fees", r.Method, r.URL.Path, nil)

	// prefer registry node pool for ethereum
	nodes := p.Reg.Best("ethereum")
	if len(nodes) == 0 {
		http.Error(w, "no healthy ETH nodes", http.StatusServiceUnavailable)
		return
	}

	// Try nodes grouped by priority with throttling
	type feeHistory struct {
		Result struct {
			BaseFeePerGas []string `json:"baseFeePerGas"`
		} `json:"result"`
	}
	type maxPrio struct {
		Result string `json:"result"`
	}

	// group by priority
	groups := map[int][]registry.NodeWithPing{}
	priorities := []int{}
	for _, n := range nodes {
		if evmThrottler.IsThrottled(n.URL) {
			continue
		}
		if _, ok := groups[n.Priority]; !ok {
			priorities = append(priorities, n.Priority)
		}
		groups[n.Priority] = append(groups[n.Priority], n)
	}
	// sort priorities ascending
	sort.Ints(priorities)

	var fh feeHistory
	var mp maxPrio
	found := false

CLIENT_LOOP:
	for _, pr := range priorities {
		for _, n := range groups[pr] {
			// Prepare requests
			client := &http.Client{Timeout: 5 * time.Second}
			target := n.URL

			req1, _ := http.NewRequest("POST", target, bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"eth_feeHistory","params":["0x1","latest",[]],"id":1}`)))
			req1.Header.Set("content-type", "application/json")
			for k, v := range n.Headers {
				req1.Header.Set(k, v)
			}
			resp1, err1 := client.Do(req1)
			if err1 != nil {
				p.Logger.Warn("networkfees_rpc_error", zap.String("upstream", secrets.RedactString(target)), zap.Error(err1))
				continue
			}
			body1, _ := io.ReadAll(resp1.Body)
			resp1.Body.Close()
			if isRateLimited(resp1, body1) {
				evmThrottler.Mark429(target)
				p.Logger.Warn("networkfees_upstream_429", zap.String("upstream", secrets.RedactString(target)))
				continue
			}
			if resp1.StatusCode/100 != 2 {
				continue
			}
			if json.Unmarshal(body1, &fh) != nil {
				continue
			}

			req2, _ := http.NewRequest("POST", target, bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"eth_maxPriorityFeePerGas","id":2}`)))
			req2.Header.Set("content-type", "application/json")
			for k, v := range n.Headers {
				req2.Header.Set(k, v)
			}
			resp2, err2 := client.Do(req2)
			if err2 != nil {
				p.Logger.Warn("networkfees_rpc_error2", zap.String("upstream", secrets.RedactString(target)), zap.Error(err2))
				continue
			}
			body2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()
			if isRateLimited(resp2, body2) {
				evmThrottler.Mark429(target)
				p.Logger.Warn("networkfees_upstream_429", zap.String("upstream", secrets.RedactString(target)))
				continue
			}
			if resp2.StatusCode/100 != 2 {
				continue
			}
			if json.Unmarshal(body2, &mp) != nil {
				continue
			}

			found = true
			break CLIENT_LOOP
		}
	}

	if !found {
		http.Error(w, "fee calc failed", http.StatusBadGateway)
		LogResponse(p.Logger, "public_network_fees", http.StatusBadGateway, nil, start)
		return
	}

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
				// Mask any secrets found in URL via secrets.RedactString
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

// GET /proxy/ethereum/fee → Tatum
// GET /proxy/ethereum/fee — try registry nodes first, fallback to ExternalURL (if configured)
// GET /proxy/ethereum/fee — try registry nodes first, fallback to ExternalURL (if configured)
func (p *Public) EthFee(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_eth_fee", r.Method, r.URL.Path, nil)
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	nodes := p.Reg.Best("ethereum")
	// try nodes grouped by priority with throttling
	if len(nodes) > 0 {
		groups := map[int][]registry.NodeWithPing{}
		priorities := []int{}
		for _, n := range nodes {
			if evmThrottler.IsThrottled(n.URL) {
				continue
			}
			if _, ok := groups[n.Priority]; !ok {
				priorities = append(priorities, n.Priority)
			}
			groups[n.Priority] = append(groups[n.Priority], n)
		}
		sort.Ints(priorities)

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

		for _, pr := range priorities {
			for _, n := range groups[pr] {
				target := n.URL
				client := &http.Client{Timeout: 5 * time.Second}

				// eth_feeHistory
				req1Body := []byte(`{"jsonrpc":"2.0","method":"eth_feeHistory","params":["0x1","latest",[]],"id":1}`)
				req1, _ := http.NewRequest("POST", target, bytes.NewReader(req1Body))
				req1.Header.Set("content-type", "application/json")
				for k, v := range n.Headers {
					req1.Header.Set(k, v)
				}
				resp1, err1 := client.Do(req1)
				if err1 != nil {
					p.Logger.Warn("eth_fee_rpc_err", zap.String("upstream", secrets.RedactString(target)), zap.Error(err1))
					continue
				}
				b1, _ := io.ReadAll(resp1.Body)
				resp1.Body.Close()
				if isRateLimited(resp1, b1) {
					evmThrottler.Mark429(target)
					p.Logger.Warn("eth_fee_upstream_429", zap.String("upstream", secrets.RedactString(target)))
					continue
				}
				if resp1.StatusCode/100 != 2 {
					continue
				}
				if json.Unmarshal(b1, &fh) != nil {
					continue
				}

				// eth_maxPriorityFeePerGas
				req2Body := []byte(`{"jsonrpc":"2.0","method":"eth_maxPriorityFeePerGas","id":2}`)
				req2, _ := http.NewRequest("POST", target, bytes.NewReader(req2Body))
				req2.Header.Set("content-type", "application/json")
				for k, v := range n.Headers {
					req2.Header.Set(k, v)
				}
				resp2, err2 := client.Do(req2)
				if err2 != nil {
					p.Logger.Warn("eth_fee_rpc_err2", zap.String("upstream", secrets.RedactString(target)), zap.Error(err2))
					continue
				}
				b2, _ := io.ReadAll(resp2.Body)
				resp2.Body.Close()
				if isRateLimited(resp2, b2) {
					evmThrottler.Mark429(target)
					p.Logger.Warn("eth_fee_upstream_429", zap.String("upstream", secrets.RedactString(target)))
					continue
				}
				if resp2.StatusCode/100 != 2 {
					continue
				}
				if json.Unmarshal(b2, &mp) != nil {
					continue
				}

				// success: build response similar to previous shape
				respBody, _ := json.Marshal(map[string]any{
					"baseFee":        first(fh.Result.BaseFeePerGas),
					"maxPriorityFee": mp.Result,
				})
				w.Header().Set("content-type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(respBody)
				LogResponse(p.Logger, "public_eth_fee", http.StatusOK, respBody, start)
				// reset throttle on success
				evmThrottler.Reset(target)
				// return from handler on first success
				return
			}
		}
	}

	// last resort: try ExternalURL from network gas config (if configured)
	if gc := p.Reg.GasConfigOf("ethereum"); gc != nil && gc.ExternalURL != "" {
		if body, code, err := (&oracle.EthGasOracle{ExternalURL: gc.ExternalURL}).FetchFromOfficial(); err == nil && code/100 == 2 {
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write(body)
			LogResponse(p.Logger, "public_eth_fee_official", code, body, start)
			return
		}
	}

	http.Error(w, "fee calc failed", http.StatusBadGateway)
	LogResponse(p.Logger, "public_eth_fee", http.StatusBadGateway, nil, start)
}

// GET /proxy/ethereum/maxPriorityFee
func (p *Public) EthMaxPriorityFee(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_eth_max_priority_fee", r.Method, r.URL.Path, nil)

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	nodes := p.Reg.Best("ethereum")
	if len(nodes) == 0 {
		http.Error(w, "no healthy ETH nodes", http.StatusServiceUnavailable)
		return
	}

	// group by priority
	groups := map[int][]registry.NodeWithPing{}
	priorities := []int{}
	for _, n := range nodes {
		if evmThrottler.IsThrottled(n.URL) {
			continue
		}
		if _, ok := groups[n.Priority]; !ok {
			priorities = append(priorities, n.Priority)
		}
		groups[n.Priority] = append(groups[n.Priority], n)
	}
	sort.Ints(priorities)

	for _, pr := range priorities {
		for _, n := range groups[pr] {
			payload := `{"jsonrpc":"2.0","id":1,"method":"eth_maxPriorityFeePerGas","params":[]}`
			client := &http.Client{Timeout: 5 * time.Second}
			req, _ := http.NewRequest(http.MethodPost, n.URL, strings.NewReader(payload))
			for k, v := range n.Headers {
				req.Header.Set(k, v)
			}
			req.Header.Set("content-type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				p.Logger.Warn("eth_maxPriority_fee_rpc_error", zap.String("upstream", secrets.RedactString(n.URL)), zap.Error(err))
				continue
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if isRateLimited(resp, b) {
				evmThrottler.Mark429(n.URL)
				p.Logger.Warn("eth_maxPriority_upstream_429", zap.String("upstream", secrets.RedactString(n.URL)))
				continue
			}
			if resp.StatusCode/100 != 2 {
				continue
			}
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(b)
			LogResponse(p.Logger, "public_eth_max_priority_fee", resp.StatusCode, b, start)
			// success -> reset throttle for this node
			evmThrottler.Reset(n.URL)
			return
		}
	}

	http.Error(w, "no healthy ETH nodes available", http.StatusBadGateway)
	LogResponse(p.Logger, "public_eth_max_priority_fee", http.StatusBadGateway, nil, start)
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
// POST /proxy/ethereum/estimateGas
func (p *Public) EthEstimateGas(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	start := LogRequest(p.Logger, "public_eth_estimate_gas", r.Method, r.URL.Path, body)

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	nodes := p.Reg.Best("ethereum")
	if len(nodes) == 0 {
		http.Error(w, "no healthy ETH nodes", http.StatusServiceUnavailable)
		return
	}

	// If client sent full JSON-RPC object (has "jsonrpc" field) — forward it as-is.
	if isJSONRPC(body) {
		tryForwardJSONRPCToNodes(p, nodes, body, start, w, "public_eth_estimate_gas")
		return
	}

	// Otherwise treat body as params (array or single params object) and create eth_estimateGas payload
	var params any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &params); err != nil {
			// Invalid client payload
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
	} else {
		params = []any{}
	}

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_estimateGas",
		"params":  params,
	}
	b, _ := json.Marshal(payload)

	// Try nodes grouped by priority with throttling (same approach as in other methods)
	groups := map[int][]registry.NodeWithPing{}
	priorities := []int{}
	for _, n := range nodes {
		if evmThrottler.IsThrottled(n.URL) {
			continue
		}
		if _, ok := groups[n.Priority]; !ok {
			priorities = append(priorities, n.Priority)
		}
		groups[n.Priority] = append(groups[n.Priority], n)
	}
	sort.Ints(priorities)

	for _, pr := range priorities {
		for _, n := range groups[pr] {
			client := &http.Client{Timeout: 8 * time.Second}
			req, _ := http.NewRequest(http.MethodPost, n.URL, bytes.NewReader(b))
			req.Header.Set("content-type", "application/json")
			for k, v := range n.Headers {
				req.Header.Set(k, v)
			}
			resp, err := client.Do(req)
			if err != nil {
				p.Logger.Warn("eth_estimate_rpc_error", zap.String("upstream", secrets.RedactString(n.URL)), zap.Error(err))
				continue
			}
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if isRateLimited(resp, respBody) {
				evmThrottler.Mark429(n.URL)
				p.Logger.Warn("eth_estimate_upstream_429", zap.String("upstream", secrets.RedactString(n.URL)))
				continue
			}
			if resp.StatusCode/100 != 2 {
				continue
			}
			// success
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(respBody)
			evmThrottler.Reset(n.URL)
			LogResponse(p.Logger, "public_eth_estimate_gas", resp.StatusCode, respBody, start)
			return
		}
	}

	// try external gas station as last resort
	if gc := p.Reg.GasConfigOf("ethereum"); gc != nil && gc.ExternalURL != "" {
		if body, code, err := (&oracle.EthGasOracle{ExternalURL: gc.ExternalURL}).FetchFromOfficial(); err == nil && code/100 == 2 {
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write(body)
			LogResponse(p.Logger, "public_eth_estimate_gas_official", code, body, start)
			return
		}
	}

	http.Error(w, "estimateGas failed", http.StatusBadGateway)
	LogResponse(p.Logger, "public_eth_estimate_gas", http.StatusBadGateway, nil, start)
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

// isJSONRPC checks whether body looks like a JSON-RPC object.
func isJSONRPC(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		if _, ok := m["jsonrpc"]; ok {
			return true
		}
	}
	return false
}

// tryForwardJSONRPCToNodes forwards raw JSON-RPC payload to nodes (grouped by priority)
// and writes first successful response back. Reuses throttling logic.
func tryForwardJSONRPCToNodes(p *Public, nodes []registry.NodeWithPing, raw []byte, started time.Time, w http.ResponseWriter, logTag string) {
	groups := map[int][]registry.NodeWithPing{}
	priorities := []int{}
	for _, n := range nodes {
		if evmThrottler.IsThrottled(n.URL) {
			continue
		}
		if _, ok := groups[n.Priority]; !ok {
			priorities = append(priorities, n.Priority)
		}
		groups[n.Priority] = append(groups[n.Priority], n)
	}
	sort.Ints(priorities)

	for _, pr := range priorities {
		for _, n := range groups[pr] {
			client := &http.Client{Timeout: 8 * time.Second}
			req, _ := http.NewRequest(http.MethodPost, n.URL, bytes.NewReader(raw))
			req.Header.Set("content-type", "application/json")
			for k, v := range n.Headers {
				req.Header.Set(k, v)
			}
			resp, err := client.Do(req)
			if err != nil {
				p.Logger.Warn(logTag+"_rpc_error", zap.String("upstream", secrets.RedactString(n.URL)), zap.Error(err))
				continue
			}
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if isRateLimited(resp, respBody) {
				evmThrottler.Mark429(n.URL)
				p.Logger.Warn(logTag+"_upstream_429", zap.String("upstream", secrets.RedactString(n.URL)))
				continue
			}
			if resp.StatusCode/100 != 2 {
				continue
			}
			// success
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(respBody)
			evmThrottler.Reset(n.URL)
			LogResponse(p.Logger, logTag, resp.StatusCode, respBody, started)
			return
		}
	}
	// none succeeded
	http.Error(w, "all upstreams failed", http.StatusBadGateway)
	LogResponse(p.Logger, logTag, http.StatusBadGateway, nil, started)
}
