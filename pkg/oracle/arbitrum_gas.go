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

type ArbitrumGasV1 struct {
	Fast             Tier  `json:"fast"`
	Standard         Tier  `json:"standard"`
	Slow             Tier  `json:"slow"`
	EstimatedBaseFee int64 `json:"estimatedBaseFee"` // L2 base fee (gwei)
	BlockNumber      int64 `json:"blockNumber"`
	// Best-effort L1 hints (если удалось получить)
	L1BaseFeeWei          string `json:"l1BaseFeeWei,omitempty"`
	L1PricingInertia      string `json:"l1PricingInertia,omitempty"`
	L1PricePerByteWei     string `json:"l1PricePerByteWei,omitempty"`
	L1PriceForCalldataWei string `json:"l1PriceForCalldataWei,omitempty"`
}

type ArbitrumGasOracle struct {
	EVM         *EVMClient
	MinPrioGwei int64
	ExternalURL string
	TTL         time.Duration
}

const (
	defaultMinPriorityGweiArb = int64(1)
	defaultTTLArb             = 10 * time.Second
	arbFastDeltaGwei          = int64(5)
	arbStdDeltaGwei           = int64(2)
	arbSlowDeltaGwei          = int64(0)
)

func NewArbitrumGasOracle(cfg *registry.NetworkState) *ArbitrumGasOracle {
	min := defaultMinPriorityGweiArb
	ttl := defaultTTLArb
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
	return &ArbitrumGasOracle{
		EVM:         NewEVMClient(2 * time.Second),
		MinPrioGwei: min,
		ExternalURL: url,
		TTL:         ttl,
	}
}

func (o *ArbitrumGasOracle) TTLDuration() time.Duration { return o.TTL }

func (o *ArbitrumGasOracle) FetchFromOfficial() ([]byte, int, error) {
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

func (o *ArbitrumGasOracle) ComputeFromRPC(node registry.NodeWithPing) (ArbitrumGasV1, error) {
	evm, err := o.EVM.Compute(node)
	if err != nil {
		return ArbitrumGasV1{}, err
	}
	prio := evm.PriorityFeeGwei
	if prio < o.MinPrioGwei {
		prio = o.MinPrioGwei
	}
	build := func(delta int64) Tier {
		p := prio + delta
		if p < o.MinPrioGwei {
			p = o.MinPrioGwei
		}
		m := evm.BaseFeeGwei + p
		return Tier{MaxPriorityFee: p, MaxFee: m, GasPrice: m}
	}

	out := ArbitrumGasV1{
		Fast:             build(arbFastDeltaGwei),
		Standard:         build(arbStdDeltaGwei),
		Slow:             build(arbSlowDeltaGwei),
		EstimatedBaseFee: evm.BaseFeeGwei,
		BlockNumber:      evm.BlockNumber,
	}

	// Best-effort: попытка получить L1 параметры через arb_gasInfo (если поддерживается)
	if l1, ok := tryArbGasInfo(node); ok {
		out.L1BaseFeeWei = l1.BaseFeeWei
		out.L1PricingInertia = l1.PricingInertia
		out.L1PricePerByteWei = l1.PricePerByteWei
		out.L1PriceForCalldataWei = l1.PriceForCalldataWei
	}

	return out, nil
}

// --- helpers ---

type arbGasInfoResult struct {
	BaseFeeWei          string
	PricingInertia      string
	PricePerByteWei     string
	PriceForCalldataWei string
}

// tryArbGasInfo: пробует нестандартный метод "arb_gasInfo" (Arbitrum Nitro).
// Возвращает true, если получилось распарсить хотя бы базовые поля.
func tryArbGasInfo(node registry.NodeWithPing) (arbGasInfoResult, bool) {
	payload := map[string]any{"jsonrpc": "2.0", "id": 1001, "method": "arb_gasInfo", "params": []any{}}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, node.URL, bytes.NewReader(b))
	for k, v := range node.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode/100 != 2 {
		return arbGasInfoResult{}, false
	}
	defer resp.Body.Close()
	var out struct {
		Result struct {
			L1BaseFeeWei          string `json:"l1BaseFeeWei"`
			L1PricingInertia      string `json:"l1PricingInertia"`
			L1PricePerByteWei     string `json:"l1PricePerByteWei"`
			L1PriceForCalldataWei string `json:"l1PriceForCalldataWei"`
		} `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return arbGasInfoResult{}, false
	}
	return arbGasInfoResult{
		BaseFeeWei:          out.Result.L1BaseFeeWei,
		PricingInertia:      out.Result.L1PricingInertia,
		PricePerByteWei:     out.Result.L1PricePerByteWei,
		PriceForCalldataWei: out.Result.L1PriceForCalldataWei,
	}, true
}
