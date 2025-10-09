package adapters

import (
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// Conservative adapter: pass explicit REST paths as is,
// add two safe scenarios: /fees and /balance/{address} (→ Tatum),
// otherwise — fallback to default behavior.
func adaptBTC(tail, method string, _ http.Header, body []byte, logger *zap.Logger) Result {
	ltail := strings.ToLower(strings.TrimPrefix(tail, "/"))

	// Пустой хвост → Blockstream, но с fallback на Tatum
	if ltail == "" || ltail == "/" {
		logger.Debug("btc_adapter_tip_height_with_fallback")
		return Result{
			Tail:              "blocks/tip/height",
			Method:            http.MethodGet,
			Body:              nil,
			Headers:           map[string]string{},
			AllowedHostSubstr: []string{"blockstream.info", "tatum.io"},
		}
	}

	if strings.HasPrefix(ltail, "rest/") ||
		strings.HasPrefix(ltail, "blocks/") ||
		strings.HasPrefix(ltail, "tx/") ||
		strings.HasPrefix(ltail, "address/") {
		return Result{
			Tail:              tail,
			Method:            method,
			Body:              clone(body),
			Headers:           map[string]string{},
			AllowedHostSubstr: []string{"blockstream.info", "tatum.io"},
		}
	}

	// fees → Tatum
	if ltail == "fees" {
		logger.Debug("btc_adapter_fees_tatum_only")
		return Result{
			Tail:              "v3/blockchain/fee/BTC",
			Method:            http.MethodGet,
			Body:              nil,
			Headers:           map[string]string{},
			AllowedHostSubstr: []string{"tatum.io"},
		}
	}

	// balance → Tatum
	if strings.HasPrefix(ltail, "balance/") {
		addr := strings.TrimPrefix(ltail, "balance/")
		if addr != "" {
			logger.Debug("btc_adapter_balance_tatum_only", zap.String("address", addr))
			return Result{
				Tail:              "v3/bitcoin/address/balance/" + addr,
				Method:            http.MethodGet,
				Body:              nil,
				Headers:           map[string]string{},
				AllowedHostSubstr: []string{"tatum.io"},
			}
		}
	}

	return Result{
		Tail:              tail,
		Method:            method,
		Body:              clone(body),
		Headers:           map[string]string{},
		AllowedHostSubstr: nil,
	}
}
