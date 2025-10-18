package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/oracle"
)

// Центральная точка входа для short-circuit эндпоинтов газа.
func (p *Proxy) TryServeGasFee(w http.ResponseWriter, r *http.Request, network, tail string) bool {
	// Обрабатываем только GET и только "*/gas"
	if r.Method != http.MethodGet || strings.ToLower(strings.Trim(tail, "/")) != "gas" {
		return false
	}

	switch strings.ToLower(network) {
	case "polygon":
		return p.servePolygonGas(w, r)
	case "ethereum":
		return p.serveEthGas(w, r)
	case "binance":
		return p.serveBscGas(w, r)
	case "arbitrum":
		return p.serveArbitrumGas(w, r)
	case "optimism":
		return p.serveOptimismGas(w, r)
	default:
		return false
	}
}

// ==================== Arbitrum ====================

var (
	arbGasOnce  sync.Once
	arbGasCache = struct {
		mu  sync.RWMutex
		b   []byte
		exp time.Time
	}{}
	arbOracle *oracle.ArbitrumGasOracle
)

// ==================== Polygon ====================

var (
	polygonGasOnce  sync.Once
	polygonGasCache = struct {
		mu  sync.RWMutex
		b   []byte
		exp time.Time
	}{}
	polygonOracle *oracle.PolygonGasOracle
)

// ==================== Ethereum ====================
var (
	ethGasOnce  sync.Once
	ethGasCache = struct {
		mu  sync.RWMutex
		b   []byte
		exp time.Time
	}{}
	ethOracle *oracle.EthGasOracle
)

// ==================== BSC ====================
var (
	bscGasOnce  sync.Once
	bscGasCache = struct {
		mu  sync.RWMutex
		b   []byte
		exp time.Time
	}{}
	bscOracle *oracle.BscGasOracle
)

// ==================== Optimism ====================

var (
	opGasOnce  sync.Once
	opGasCache = struct {
		mu  sync.RWMutex
		b   []byte
		exp time.Time
	}{}
	opOracle *oracle.OptimismGasOracle
)

func (p *Proxy) servePolygonGas(w http.ResponseWriter, r *http.Request) bool {
	start := LogRequest(p.Logger, "proxy_polygon_gas", r.Method, r.URL.Path, nil)

	// Lazy init провайдера на основе конфигурации из Registry
	polygonGasOnce.Do(func() {
		// Достаём весь NetworkState для polygon (там теперь есть Gas)
		all := p.Reg.All()
		if st, ok := all["polygon"]; ok {
			polygonOracle = oracle.NewPolygonGasOracle(st)
		} else {
			// fallback на дефолты, если почему-то сети нет в реестре
			polygonOracle = oracle.NewPolygonGasOracle(nil)
		}
	})

	// 1) Cache
	polygonGasCache.mu.RLock()
	if polygonGasCache.b != nil && time.Now().Before(polygonGasCache.exp) {
		cached := make([]byte, len(polygonGasCache.b))
		copy(cached, polygonGasCache.b)
		polygonGasCache.mu.RUnlock()

		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached)
		LogResponse(p.Logger, "proxy_polygon_gas_cache", http.StatusOK, cached, start)
		return true
	}
	polygonGasCache.mu.RUnlock()

	// 2) Внешний Gas Station (опционально)
	if body, code, err := polygonOracle.FetchFromOfficial(); err == nil && code/100 == 2 {
		polygonGasCache.mu.Lock()
		polygonGasCache.b = append([]byte{}, body...)
		polygonGasCache.exp = time.Now().Add(polygonOracle.TTLDuration())
		polygonGasCache.mu.Unlock()

		w.Header().Set("content-type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(body)
		LogResponse(p.Logger, "proxy_polygon_gas_official", code, body, start)
		return true
	}

	// 3) Локальный расчёт через RPC
	nodes := p.Reg.Best("polygon")
	if len(nodes) == 0 {
		http.Error(w, "no polygon nodes", http.StatusServiceUnavailable)
		LogResponse(p.Logger, "proxy_polygon_gas_no_nodes", http.StatusServiceUnavailable, nil, start)
		return true
	}

	var out []byte
	for _, n := range nodes {
		if st, err := polygonOracle.ComputeFromRPC(n); err == nil {
			if b, err2 := json.Marshal(st); err2 == nil {
				out = b
				break
			}
		}
	}
	if len(out) == 0 {
		http.Error(w, "gas calc failed", http.StatusBadGateway)
		LogResponse(p.Logger, "proxy_polygon_gas_calc_failed", http.StatusBadGateway, nil, start)
		return true
	}

	polygonGasCache.mu.Lock()
	polygonGasCache.b = append([]byte{}, out...)
	polygonGasCache.exp = time.Now().Add(polygonOracle.TTLDuration())
	polygonGasCache.mu.Unlock()

	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
	LogResponse(p.Logger, "proxy_polygon_gas_local", http.StatusOK, out, start)
	return true
}
func (p *Proxy) serveEthGas(w http.ResponseWriter, r *http.Request) bool {
	start := LogRequest(p.Logger, "proxy_eth_gas", r.Method, r.URL.Path, nil)

	ethGasOnce.Do(func() {
		all := p.Reg.All()
		if st, ok := all["ethereum"]; ok {
			ethOracle = oracle.NewEthGasOracle(st)
		} else {
			ethOracle = oracle.NewEthGasOracle(nil)
		}
	})

	ttl := ethOracle.TTLDuration()

	ethGasCache.mu.RLock()
	if ethGasCache.b != nil && time.Now().Before(ethGasCache.exp) {
		cached := make([]byte, len(ethGasCache.b))
		copy(cached, ethGasCache.b)
		ethGasCache.mu.RUnlock()

		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached)
		LogResponse(p.Logger, "proxy_eth_gas_cache", http.StatusOK, cached, start)
		return true
	}
	ethGasCache.mu.RUnlock()

	if body, code, err := ethOracle.FetchFromOfficial(); err == nil && code/100 == 2 {
		ethGasCache.mu.Lock()
		ethGasCache.b = append([]byte{}, body...)
		ethGasCache.exp = time.Now().Add(ttl)
		ethGasCache.mu.Unlock()

		w.Header().Set("content-type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(body)
		LogResponse(p.Logger, "proxy_eth_gas_official", code, body, start)
		return true
	}

	nodes := p.Reg.Best("ethereum")
	if len(nodes) == 0 {
		http.Error(w, "no ethereum nodes", http.StatusServiceUnavailable)
		LogResponse(p.Logger, "proxy_ethereum_gas_no_nodes", http.StatusServiceUnavailable, nil, start)
		return true
	}

	var out []byte
	for _, n := range nodes {
		if st, err := ethOracle.ComputeFromRPC(n); err == nil {
			if b, err2 := json.Marshal(st); err2 == nil {
				out = b
				break
			}
		}
	}
	if len(out) == 0 {
		http.Error(w, "gas calc failed", http.StatusBadGateway)
		LogResponse(p.Logger, "proxy_ethereum_gas_calc_failed", http.StatusBadGateway, nil, start)
		return true
	}

	ethGasCache.mu.Lock()
	ethGasCache.b = append([]byte{}, out...)
	ethGasCache.exp = time.Now().Add(ttl)
	ethGasCache.mu.Unlock()

	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
	LogResponse(p.Logger, "proxy_ethereum_gas_local", http.StatusOK, out, start)
	return true
}
func (p *Proxy) serveBscGas(w http.ResponseWriter, r *http.Request) bool {
	start := LogRequest(p.Logger, "proxy_binance_gas", r.Method, r.URL.Path, nil)

	// Lazy init
	bscGasOnce.Do(func() {
		all := p.Reg.All()
		if st, ok := all["binance"]; ok {
			bscOracle = oracle.NewBscGasOracle(st)
		} else {
			bscOracle = oracle.NewBscGasOracle(nil)
		}
	})
	ttl := bscOracle.TTLDuration()

	// Cache
	bscGasCache.mu.RLock()
	if bscGasCache.b != nil && time.Now().Before(bscGasCache.exp) {
		cached := append([]byte(nil), bscGasCache.b...)
		bscGasCache.mu.RUnlock()
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached)
		LogResponse(p.Logger, "proxy_binance_gas_cache", http.StatusOK, cached, start)
		return true
	}
	bscGasCache.mu.RUnlock()

	// External
	if body, code, err := bscOracle.FetchFromOfficial(); err == nil && code/100 == 2 {
		bscGasCache.mu.Lock()
		bscGasCache.b = append([]byte{}, body...)
		bscGasCache.exp = time.Now().Add(ttl)
		bscGasCache.mu.Unlock()

		w.Header().Set("content-type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(body)
		LogResponse(p.Logger, "proxy_binance_gas_official", code, body, start)
		return true
	}

	// Local compute
	nodes := p.Reg.Best("binance")
	if len(nodes) == 0 {
		http.Error(w, "no binance nodes", http.StatusServiceUnavailable)
		LogResponse(p.Logger, "proxy_binance_gas_no_nodes", http.StatusServiceUnavailable, nil, start)
		return true
	}
	var out []byte
	for _, n := range nodes {
		if st, err := bscOracle.ComputeFromRPC(n); err == nil {
			if b, err2 := json.Marshal(st); err2 == nil {
				out = b
				break
			}
		}
	}
	if len(out) == 0 {
		http.Error(w, "gas calc failed", http.StatusBadGateway)
		LogResponse(p.Logger, "proxy_binance_gas_calc_failed", http.StatusBadGateway, nil, start)
		return true
	}
	bscGasCache.mu.Lock()
	bscGasCache.b = append([]byte{}, out...)
	bscGasCache.exp = time.Now().Add(ttl)
	bscGasCache.mu.Unlock()

	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
	LogResponse(p.Logger, "proxy_binance_gas_local", http.StatusOK, out, start)
	return true
}
func (p *Proxy) serveArbitrumGas(w http.ResponseWriter, r *http.Request) bool {
	start := LogRequest(p.Logger, "proxy_arbitrum_gas", r.Method, r.URL.Path, nil)

	arbGasOnce.Do(func() {
		all := p.Reg.All()
		if st, ok := all["arbitrum"]; ok {
			arbOracle = oracle.NewArbitrumGasOracle(st)
		} else {
			arbOracle = oracle.NewArbitrumGasOracle(nil)
		}
	})
	ttl := arbOracle.TTLDuration()

	arbGasCache.mu.RLock()
	if arbGasCache.b != nil && time.Now().Before(arbGasCache.exp) {
		cached := append([]byte(nil), arbGasCache.b...)
		arbGasCache.mu.RUnlock()
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached)
		LogResponse(p.Logger, "proxy_arbitrum_gas_cache", http.StatusOK, cached, start)
		return true
	}
	arbGasCache.mu.RUnlock()

	if body, code, err := arbOracle.FetchFromOfficial(); err == nil && code/100 == 2 {
		arbGasCache.mu.Lock()
		arbGasCache.b = append([]byte{}, body...)
		arbGasCache.exp = time.Now().Add(ttl)
		arbGasCache.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(body)
		LogResponse(p.Logger, "proxy_arbitrum_gas_official", code, body, start)
		return true
	}

	nodes := p.Reg.Best("arbitrum")
	if len(nodes) == 0 {
		http.Error(w, "no arbitrum nodes", http.StatusServiceUnavailable)
		LogResponse(p.Logger, "proxy_arbitrum_gas_no_nodes", http.StatusServiceUnavailable, nil, start)
		return true
	}
	var out []byte
	for _, n := range nodes {
		if st, err := arbOracle.ComputeFromRPC(n); err == nil {
			if b, err2 := json.Marshal(st); err2 == nil {
				out = b
				break
			}
		}
	}
	if len(out) == 0 {
		http.Error(w, "gas calc failed", http.StatusBadGateway)
		LogResponse(p.Logger, "proxy_arbitrum_gas_calc_failed", http.StatusBadGateway, nil, start)
		return true
	}

	arbGasCache.mu.Lock()
	arbGasCache.b = append([]byte{}, out...)
	arbGasCache.exp = time.Now().Add(ttl)
	arbGasCache.mu.Unlock()

	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
	LogResponse(p.Logger, "proxy_arbitrum_gas_local", http.StatusOK, out, start)
	return true
}
func (p *Proxy) serveOptimismGas(w http.ResponseWriter, r *http.Request) bool {
	start := LogRequest(p.Logger, "proxy_optimism_gas", r.Method, r.URL.Path, nil)

	opGasOnce.Do(func() {
		all := p.Reg.All()
		if st, ok := all["optimism"]; ok {
			opOracle = oracle.NewOptimismGasOracle(st)
		} else {
			opOracle = oracle.NewOptimismGasOracle(nil)
		}
	})
	ttl := opOracle.TTLDuration()

	opGasCache.mu.RLock()
	if opGasCache.b != nil && time.Now().Before(opGasCache.exp) {
		cached := append([]byte(nil), opGasCache.b...)
		opGasCache.mu.RUnlock()
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached)
		LogResponse(p.Logger, "proxy_optimism_gas_cache", http.StatusOK, cached, start)
		return true
	}
	opGasCache.mu.RUnlock()

	if body, code, err := opOracle.FetchFromOfficial(); err == nil && code/100 == 2 {
		opGasCache.mu.Lock()
		opGasCache.b = append([]byte{}, body...)
		opGasCache.exp = time.Now().Add(ttl)
		opGasCache.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(body)
		LogResponse(p.Logger, "proxy_optimism_gas_official", code, body, start)
		return true
	}

	nodes := p.Reg.Best("optimism")
	if len(nodes) == 0 {
		http.Error(w, "no optimism nodes", http.StatusServiceUnavailable)
		LogResponse(p.Logger, "proxy_optimism_gas_no_nodes", http.StatusServiceUnavailable, nil, start)
		return true
	}
	var out []byte
	for _, n := range nodes {
		if st, err := opOracle.ComputeFromRPC(n); err == nil {
			if b, err2 := json.Marshal(st); err2 == nil {
				out = b
				break
			}
		}
	}
	if len(out) == 0 {
		http.Error(w, "gas calc failed", http.StatusBadGateway)
		LogResponse(p.Logger, "proxy_optimism_gas_calc_failed", http.StatusBadGateway, nil, start)
		return true
	}

	opGasCache.mu.Lock()
	opGasCache.b = append([]byte{}, out...)
	opGasCache.exp = time.Now().Add(ttl)
	opGasCache.mu.Unlock()

	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
	LogResponse(p.Logger, "proxy_optimism_gas_local", http.StatusOK, out, start)
	return true
}
