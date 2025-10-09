package oracle

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/registry"
)

type EthGasV1 struct {
	Fast             Tier  `json:"fast"`
	Standard         Tier  `json:"standard"`
	Slow             Tier  `json:"slow"`
	EstimatedBaseFee int64 `json:"estimatedBaseFee"`
	BlockNumber      int64 `json:"blockNumber"`
}

type EthGasOracle struct {
	EVM         *EVMClient
	MinPrioGwei int64
	ExternalURL string
	TTL         time.Duration
}

const (
	defaultMinPriorityGweiEth = int64(2)
	defaultTTLEth             = 10 * time.Second
)

func NewEthGasOracle(cfg *registry.NetworkState) *EthGasOracle {
	min := defaultMinPriorityGweiEth
	ttl := defaultTTLEth
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
	return &EthGasOracle{
		EVM:         NewEVMClient(2 * time.Second),
		MinPrioGwei: min,
		ExternalURL: url,
		TTL:         ttl,
	}
}

func (o *EthGasOracle) ComputeFromRPC(node registry.NodeWithPing) (EthGasV1, error) {
	evm, err := o.EVM.Compute(node)
	if err != nil {
		return EthGasV1{}, err
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
	return EthGasV1{
		Fast:             build(10),
		Standard:         build(5),
		Slow:             build(0),
		EstimatedBaseFee: evm.BaseFeeGwei,
		BlockNumber:      evm.BlockNumber,
	}, nil
}

func (o *EthGasOracle) FetchFromOfficial() ([]byte, int, error) {
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

func (o *EthGasOracle) TTLDuration() time.Duration { return o.TTL }
