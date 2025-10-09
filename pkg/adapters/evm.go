package adapters

import (
	"net/http"
	"strings"

	"go.uber.org/zap"
)

func adaptEVM(tail, method string, _ http.Header, body []byte, logger *zap.Logger) Result {
	ltail := strings.ToLower(strings.TrimPrefix(tail, "/"))

	// Поддержка удобных GET-маршрутов
	if method == http.MethodGet {
		switch {
		case ltail == "blocknumber" || ltail == "block_number":
			logger.Debug("evm_adapter_blocknumber_get")
			return Result{
				Tail:   "", // JSON-RPC на корень
				Method: http.MethodPost,
				Body: mustJSON(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"method":  "eth_blockNumber",
					"params":  []any{},
				}),
				Headers: ensureJSON(nil),
			}

		case ltail == "gasprice" || ltail == "gas_price":
			logger.Debug("evm_adapter_gasprice_get")
			return Result{
				Tail:   "",
				Method: http.MethodPost,
				Body: mustJSON(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"method":  "eth_gasPrice",
					"params":  []any{},
				}),
				Headers: ensureJSON(nil),
			}

		case ltail == "chainid" || ltail == "chain_id":
			logger.Debug("evm_adapter_chainid_get")
			return Result{
				Tail:   "",
				Method: http.MethodPost,
				Body: mustJSON(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"method":  "eth_chainId",
					"params":  []any{},
				}),
				Headers: ensureJSON(nil),
			}

		case strings.HasPrefix(ltail, "balance/"):
			addr := strings.TrimPrefix(ltail, "balance/")
			if addr != "" {
				logger.Debug("evm_adapter_balance_get", zap.String("address", addr))
				return Result{
					Tail:   "",
					Method: http.MethodPost,
					Body: mustJSON(map[string]any{
						"jsonrpc": "2.0",
						"id":      1,
						"method":  "eth_getBalance",
						"params":  []any{normalizeHex(addr), "latest"},
					}),
					Headers: ensureJSON(nil),
				}
			}
		}
	}

	// Иначе — поведение по умолчанию (как было)
	return Result{
		Tail:    tail,
		Method:  method,
		Body:    clone(body),
		Headers: map[string]string{},
	}
}
