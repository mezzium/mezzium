package oracle

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/registry"
)

type PolygonGasV2 struct {
	Fast             Tier  `json:"fast"`
	Standard         Tier  `json:"standard"`
	Slow             Tier  `json:"slow"`
	EstimatedBaseFee int64 `json:"estimatedBaseFee"`
	BlockNumber      int64 `json:"blockNumber"`
}

type PolygonGasOracle struct {
	EVM         *EVMClient
	MinPrioGwei int64
	ExternalURL string
	TTL         time.Duration
}

const (
	defaultMinPriorityGwei = int64(30)
	defaultTTL             = 15 * time.Second
	fastDeltaGwei          = int64(10)
	standardDeltaGwei      = int64(5)
	slowDeltaGwei          = int64(0)
)

func NewPolygonGasOracle(gasCfg *registry.NetworkState) *PolygonGasOracle {
	// gasCfg может быть nil; защищаем дефолтами
	minPrio := defaultMinPriorityGwei
	ttl := defaultTTL
	external := ""
	if gasCfg != nil && gasCfg.Gas != nil {
		if gasCfg.Gas.MinPriorityGwei > 0 {
			minPrio = gasCfg.Gas.MinPriorityGwei
		}
		if gasCfg.Gas.TTLSeconds > 0 {
			ttl = time.Duration(gasCfg.Gas.TTLSeconds) * time.Second
		}
		if gasCfg.Gas.ExternalURL != "" {
			external = gasCfg.Gas.ExternalURL
		}
	}
	return &PolygonGasOracle{
		EVM:         NewEVMClient(2 * time.Second),
		MinPrioGwei: minPrio,
		ExternalURL: external,
		TTL:         ttl,
	}
}

// ComputeFromRPC — локальный расчёт на одной RPC-ноде
func (o *PolygonGasOracle) ComputeFromRPC(node registry.NodeWithPing) (PolygonGasV2, error) {
	evm, err := o.EVM.Compute(node)
	if err != nil {
		return PolygonGasV2{}, err
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
	return PolygonGasV2{
		Fast:             build(fastDeltaGwei),
		Standard:         build(standardDeltaGwei),
		Slow:             build(slowDeltaGwei),
		EstimatedBaseFee: evm.BaseFeeGwei,
		BlockNumber:      evm.BlockNumber,
	}, nil
}

// FetchFromOfficial — пробует внешний Gas Station; если ExternalURL пуст — возвращает ошибку
func (o *PolygonGasOracle) FetchFromOfficial() ([]byte, int, error) {
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

func (o *PolygonGasOracle) TTLDuration() time.Duration { return o.TTL }
