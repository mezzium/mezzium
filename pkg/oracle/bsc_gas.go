package oracle

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/registry"
)

type BscGasV1 struct {
	Fast             Tier  `json:"fast"`
	Standard         Tier  `json:"standard"`
	Slow             Tier  `json:"slow"`
	EstimatedBaseFee int64 `json:"estimatedBaseFee"`
	BlockNumber      int64 `json:"blockNumber"`
}

type BscGasOracle struct {
	EVM         *EVMClient
	MinPrioGwei int64
	ExternalURL string
	TTL         time.Duration
}

const (
	defaultMinPriorityGweiBSC = int64(1)
	defaultTTLBSC             = 10 * time.Second

	// дельты для fast/standard/slow поверх priority
	bscFastDeltaGwei     = int64(5)
	bscStandardDeltaGwei = int64(2)
	bscSlowDeltaGwei     = int64(0)
)

func NewBscGasOracle(cfg *registry.NetworkState) *BscGasOracle {
	min := defaultMinPriorityGweiBSC
	ttl := defaultTTLBSC
	url := ""
	if cfg != nil && cfg.Gas != nil {
		if cfg.Gas.MinPriorityGwei > 0 {
			min = cfg.Gas.MinPriorityGwei
		}
		if cfg.Gas.TTLSeconds > 0 {
			ttl = time.Duration(cfg.Gas.TTLSeconds) * time.Second
		}
		if cfg.Gas.ExternalURL != "" {
			url = cfg.Gas.ExternalURL
		}
	}
	return &BscGasOracle{
		EVM:         NewEVMClient(2 * time.Second),
		MinPrioGwei: min,
		ExternalURL: url,
		TTL:         ttl,
	}
}

func (o *BscGasOracle) TTLDuration() time.Duration { return o.TTL }

// ComputeFromRPC — локальный расчёт. Особенность BSC: некоторые RPC не поддерживают eth_feeHistory.
// Алгоритм: пробуем EIP-1559 через EVMClient; если падает baseFee — используем eth_gasPrice и считаем приближённо:
//
//	base ≈ gasPrice - max(minPrio, observedPrio), clamp ≥ 0; maxFee ≈ base + prio.
func (o *BscGasOracle) ComputeFromRPC(node registry.NodeWithPing) (BscGasV1, error) {
	evm, err := o.EVM.Compute(node)
	var baseGwei, prioGwei, block int64
	if err == nil && evm.BaseFeeGwei > 0 {
		baseGwei = evm.BaseFeeGwei
		prioGwei = evm.PriorityFeeGwei
		block = evm.BlockNumber
	} else {
		// fallback: gasPrice + blockNumber напрямую
		gp, _ := o.ethGasPriceGwei(node)
		blk, _ := o.blockNumber(node)
		block = blk

		// попытка получить priority отдельно; если нет — берём минимум
		pr, prErr := o.priorityFee(node)
		if prErr != nil || pr <= 0 {
			pr = o.MinPrioGwei
		}
		if pr < o.MinPrioGwei {
			pr = o.MinPrioGwei
		}
		// base ≈ gasPrice - prio (не отрицательный)
		base := gp - pr
		if base < 0 {
			base = 0
		}
		baseGwei, prioGwei = base, pr
	}

	if prioGwei < o.MinPrioGwei {
		prioGwei = o.MinPrioGwei
	}

	build := func(delta int64) Tier {
		p := prioGwei + delta
		if p < o.MinPrioGwei {
			p = o.MinPrioGwei
		}
		m := baseGwei + p
		return Tier{MaxPriorityFee: p, MaxFee: m, GasPrice: m}
	}

	return BscGasV1{
		Fast:             build(bscFastDeltaGwei),
		Standard:         build(bscStandardDeltaGwei),
		Slow:             build(bscSlowDeltaGwei),
		EstimatedBaseFee: baseGwei,
		BlockNumber:      block,
	}, nil
}

func (o *BscGasOracle) FetchFromOfficial() ([]byte, int, error) {
	if o.ExternalURL == "" {
		return nil, 0, errors.New("no external gas url configured")
	}
	req, _ := http.NewRequest(http.MethodGet, o.ExternalURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode, nil
}

// --- local helpers (RPC calls) ---

func (o *BscGasOracle) rpc(node registry.NodeWithPing, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, node.URL, bytes.NewReader(b))
	for k, v := range node.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return simpleErr("rpc non-2xx")
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (o *BscGasOracle) ethGasPriceGwei(node registry.NodeWithPing) (int64, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": 11, "method": "eth_gasPrice", "params": []any{}}
	var out struct {
		Result string `json:"result"`
	}
	if err := o.rpc(node, req, &out); err != nil {
		return 0, err
	}
	return hexWeiToGwei(out.Result), nil
}

func (o *BscGasOracle) priorityFee(node registry.NodeWithPing) (int64, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": 12, "method": "eth_maxPriorityFeePerGas", "params": []any{}}
	var out struct {
		Result string `json:"result"`
	}
	if err := o.rpc(node, req, &out); err != nil {
		return 0, err
	}
	return hexWeiToGwei(out.Result), nil
}

func (o *BscGasOracle) blockNumber(node registry.NodeWithPing) (int64, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": 13, "method": "eth_blockNumber", "params": []any{}}
	var out struct {
		Result string `json:"result"`
	}
	if err := o.rpc(node, req, &out); err != nil {
		return 0, err
	}
	return hexToInt64(out.Result), nil
}
