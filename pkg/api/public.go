package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/adapters"
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

	http.Error(w, "fee calc failed", http.StatusBadGateway)
	LogResponse(p.Logger, "public_eth_fee", http.StatusBadGateway, nil, start)
}

// EthMaxPriorityFee GET /ethereum/maxPriorityFee
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

// NFTGetAllNFTs GET /nft/get-all-nfts/{address} → через адаптер nft (EVM RPC)
func (p *Public) NFTGetAllNFTs(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_nft_get_all", r.Method, r.URL.Path, nil)
	const prefix = "/ethereum/nft/get-all-nfts/"
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	owner := strings.TrimPrefix(r.URL.Path, prefix)
	if owner == "" {
		http.Error(w, "owner required", http.StatusBadRequest)
		return
	}
	contract := strings.TrimSpace(r.URL.Query().Get("contract"))
	if contract == "" {
		http.Error(w, "contract required", http.StatusBadRequest)
		return
	}
	limit := parseUintDefault(r.URL.Query().Get("limit"), 50)
	offset := parseUintDefault(r.URL.Query().Get("offset"), 0)

	nodes := p.Reg.Best("ethereum")
	if len(nodes) == 0 {
		http.Error(w, "no healthy ETH nodes", http.StatusServiceUnavailable)
		return
	}
	// Step 1: supportsInterface(0x780e9d63) — ERC721Enumerable
	siData := adapters.BuildSupportsInterfaceData("0x780e9d63")
	siPayload := mustJSONRPC("eth_call", []any{
		map[string]string{"to": adapters.NormalizeHex(contract), "data": siData},
		"latest",
	})
	if !forwardFirst2xx(p, nodes, siPayload, start, "public_nft_supports") {
		http.Error(w, "all upstreams failed", http.StatusBadGateway)
		return
	}
	// We don't decode ABI here; we assume clients can call this endpoint knowing enumerable requirement.
	// Step 2: balanceOf(owner)
	balData := adapters.BuildBalanceOfData(owner)
	balPayload := mustJSONRPC("eth_call", []any{
		map[string]string{"to": adapters.NormalizeHex(contract), "data": balData},
		"latest",
	})
	balResp := callFirst2xx(p, nodes, balPayload, start, "public_nft_balance")
	if len(balResp) == 0 {
		http.Error(w, "failed to get balance", http.StatusBadGateway)
		return
	}
	total := tryParseHexQuantityFromRPC(balResp)
	items := make([]string, 0, minUint(limit, total-offset))
	max := minUint(limit, total-offset)
	for i := uint64(0); i < max; i++ {
		idx := offset + i
		tokData := adapters.BuildTokenOfOwnerByIndexData(owner, idx)
		tokPayload := mustJSONRPC("eth_call", []any{
			map[string]string{"to": adapters.NormalizeHex(contract), "data": tokData},
			"latest",
		})
		resp := callFirst2xx(p, nodes, tokPayload, start, "public_nft_token_by_index")
		if len(resp) == 0 {
			continue
		}
		// Extract "result" hex string (e.g. "0x1234...") and append as string
		if hex := extractResultHexString(resp); hex != "" {
			items = append(items, hex)
		}
	}
	out := map[string]any{
		"contract": adapters.NormalizeHex(contract),
		"owner":    adapters.NormalizeHex(owner),
		"total":    total,
		"items":    items,
	}
	writeJSON(w, http.StatusOK, out)
	LogResponse(p.Logger, "public_nft_get_all", http.StatusOK, nil, start)
}

// NFTGetNFTMetadata GET /ethereum/nft/get-nft-metadata/{contract}/{tokenId} → tokenURI via RPC
func (p *Public) NFTGetNFTMetadata(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_nft_get_metadata", r.Method, r.URL.Path, nil)
	const prefix = "/ethereum/nft/get-nft-metadata/"
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

	nodes := p.Reg.Best("ethereum")
	if len(nodes) == 0 {
		http.Error(w, "no healthy ETH nodes", http.StatusServiceUnavailable)
		return
	}

	data := adapters.BuildTokenURIData(tokenId)
	payload := mustJSONRPC("eth_call", []any{
		map[string]string{"to": adapters.NormalizeHex(contract), "data": data},
		"latest",
	})
	tryForwardJSONRPCToNodes(p, nodes, payload, start, w, "public_nft_get_metadata")
}

// BTCBalance GET /btc/balance/{address} → через пул BTC‑нод
func (p *Public) BTCBalance(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(p.Logger, "public_btc_balance", r.Method, r.URL.Path, nil)
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	const prefix = "/btc/balance/"
	addr := strings.TrimPrefix(r.URL.Path, prefix)
	if addr == "" {
		http.Error(w, "address required", http.StatusBadRequest)
		return
	}

	// формируем JSON‑RPC getbalance (или REST balance) через адаптер
	raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"getbalance","params":["` + addr + `"]}`)
	nodes := p.Reg.Best("btc")
	if len(nodes) == 0 {
		http.Error(w, "no healthy BTC nodes", http.StatusServiceUnavailable)
		return
	}
	tryForwardJSONRPCToNodes(p, nodes, raw, start, w, "public_btc_balance")
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

// forwardFirst2xx: send payload to nodes and return true if any succeeded
func forwardFirst2xx(p *Public, nodes []registry.NodeWithPing, raw []byte, started time.Time, logTag string) bool {
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
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if isRateLimited(resp, body) {
				evmThrottler.Mark429(n.URL)
				p.Logger.Warn(logTag+"_upstream_429", zap.String("upstream", secrets.RedactString(n.URL)))
				continue
			}
			if resp.StatusCode/100 != 2 {
				continue
			}
			evmThrottler.Reset(n.URL)
			LogResponse(p.Logger, logTag, resp.StatusCode, body, started)
			return true
		}
	}
	return false
}

// callFirst2xx: returns raw JSON-RPC body of first success
func callFirst2xx(p *Public, nodes []registry.NodeWithPing, raw []byte, started time.Time, logTag string) []byte {
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
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if isRateLimited(resp, body) {
				evmThrottler.Mark429(n.URL)
				p.Logger.Warn(logTag+"_upstream_429", zap.String("upstream", secrets.RedactString(n.URL)))
				continue
			}
			if resp.StatusCode/100 != 2 {
				continue
			}
			evmThrottler.Reset(n.URL)
			LogResponse(p.Logger, logTag, resp.StatusCode, body, started)
			return body
		}
	}
	return nil
}

func mustJSONRPC(method string, params []any) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	return b
}

// tryParseHexQuantityFromRPC pulls "result" and returns uint64 (as string when needed)
func tryParseHexQuantityFromRPC(b []byte) uint64 {
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return 0
	}
	r, _ := out["result"].(string)
	r = strings.TrimPrefix(strings.ToLower(r), "0x")
	if r == "" {
		return 0
	}
	n := new(big.Int)
	n.SetString(r, 16)
	return n.Uint64()
}

func respHexOnly(b []byte) []byte {
	// return hex string in "result" as plain string for appending
	return b
}

func minUint(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func parseUintDefault(s string, def uint64) uint64 {
	if s == "" {
		return def
	}
	var v uint64
	_, err := fmt.Sscanf(s, "%d", &v)
	if err != nil {
		return def
	}
	return v
}

// extractResultHexString returns the "result" field from a JSON-RPC response as the original hex string (with 0x prefix).
// If the response cannot be parsed or result is empty, returns empty string.
func extractResultHexString(b []byte) string {
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return ""
	}
	r, ok := out["result"].(string)
	if !ok || r == "" {
		return ""
	}
	// normalize to 0x + lower-case hex
	if !strings.HasPrefix(strings.ToLower(r), "0x") {
		return "0x" + strings.ToLower(r)
	}
	return strings.ToLower(r)
}
func first(arr []string) string {
	if len(arr) == 0 {
		return ""
	}
	return arr[len(arr)-1]
}
