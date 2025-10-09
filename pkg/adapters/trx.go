package adapters

import (
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// Rules:
// - /wallet/* and /v1/* are passed as is;
// - /balance/{address} → TronGrid /v1/accounts/{address} (GET);
// - otherwise: default behaviour — wallet/getnowblock.
func adaptTRX(tail, method string, _ http.Header, body []byte, logger *zap.Logger) Result {
	ltail := strings.ToLower(strings.TrimPrefix(tail, "/"))

	// 1) extension: balance TRX via TronGrid
	if strings.HasPrefix(ltail, "balance/") {
		addr := strings.TrimPrefix(ltail, "balance/")
		if addr != "" {
			logger.Debug("trx_adapter_balance_trongrid_only", zap.String("address", addr))
			return Result{
				Tail:              "v1/accounts/" + addr,
				Method:            http.MethodGet,
				Body:              nil,
				Headers:           ensureJSON(nil),
				AllowedHostSubstr: []string{"trongrid.io"},
			}
		}
	}

	// 2) default path TronGrid/wallet — as is
	if strings.HasPrefix(ltail, "wallet/") ||
		strings.HasPrefix(ltail, "walletsolidity/") ||
		strings.HasPrefix(ltail, "v1/") {
		return Result{
			Tail:              tail,
			Method:            method,
			Body:              clone(body),
			Headers:           ensureJSON(nil),
			AllowedHostSubstr: nil,
		}
	}

	// 3) Default: universal proxy
	return Result{
		Tail:    tail,
		Method:  method,
		Body:    clone(body),
		Headers: ensureJSON(nil),
	}
}
