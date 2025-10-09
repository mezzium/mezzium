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

type OptimismGasV1 struct {
	Fast             Tier  `json:"fast"`
	Standard         Tier  `json:"standard"`
	Slow             Tier  `json:"slow"`
	EstimatedBaseFee int64 `json:"estimatedBaseFee"` // L2 base fee (gwei)
	BlockNumber      int64 `json:"blockNumber"`
	// Best-effort L1 hints (если удалось получить)
	L1BaseFeeWei string `json:"l1BaseFeeWei,omitempty"`
	L2BaseFeeWei string `json:"l2BaseFeeWei,omitempty"`
}

type OptimismGasOracle struct {
	EVM         *EVMClient
	MinPrioGwei int64
	ExternalURL string
	TTL         time.Duration
}

const (
	defaultMinPriorityGweiOp = int64(1)
	defaultTTLOp             = 10 * time.Second
	opFastDeltaGwei          = int64(5)
	opStdDeltaGwei           = int64(2)
	opSlowDeltaGwei          = int64(0)
)

func NewOptimismGasOracle(cfg *registry.NetworkState) *OptimismGasOracle {
	min := defaultMinPriorityGweiOp
	ttl := defaultTTLOp
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
	return &OptimismGasOracle{
		EVM:         NewEVMClient(2 * time.Second),
		MinPrioGwei: min,
		ExternalURL: url,
		TTL:         ttl,
	}
}

func (o *OptimismGasOracle) TTLDuration() time.Duration { return o.TTL }

func (o *OptimismGasOracle) FetchFromOfficial() ([]byte, int, error) {
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

func (o *OptimismGasOracle) ComputeFromRPC(node registry.NodeWithPing) (OptimismGasV1, error) {
	evm, err := o.EVM.Compute(node)
	if err != nil {
		return OptimismGasV1{}, err
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

	out := OptimismGasV1{
		Fast:             build(opFastDeltaGwei),
		Standard:         build(opStdDeltaGwei),
		Slow:             build(opSlowDeltaGwei),
		EstimatedBaseFee: evm.BaseFeeGwei,
		BlockNumber:      evm.BlockNumber,
	}

	// Best-effort: rollup_gasPrices (Bedrock) — возвращает l1BaseFee/l2BaseFee в wei
	if l1, l2, ok := tryOptimismRollupGasPrices(node); ok {
		out.L1BaseFeeWei = l1
		out.L2BaseFeeWei = l2
	}

	return out, nil
}

// tryOptimismRollupGasPrices вызывает нестандартный метод "rollup_gasPrices".
func tryOptimismRollupGasPrices(node registry.NodeWithPing) (l1Wei string, l2Wei string, ok bool) {
	payload := map[string]any{"jsonrpc": "2.0", "id": 1002, "method": "rollup_gasPrices", "params": []any{}}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, node.URL, bytes.NewReader(b))
	for k, v := range node.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode/100 != 2 {
		return "", "", false
	}
	defer resp.Body.Close()
	var out struct {
		Result struct {
			L1BaseFee string `json:"l1BaseFee"`
			L2BaseFee string `json:"l2BaseFee"`
		} `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || (out.Result.L1BaseFee == "" && out.Result.L2BaseFee == "") {
		return "", "", false
	}
	return out.Result.L1BaseFee, out.Result.L2BaseFee, true
}
