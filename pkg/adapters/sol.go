package adapters

import (
	"encoding/json"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

func adaptSOL(tail, method string, _ http.Header, body []byte, logger *zap.Logger) Result {
	ltail := strings.ToLower(strings.TrimPrefix(tail, "/"))

	// Уважение GET: только известные шорткаты преобразуем, остальное — пробрасываем как есть
	if method == http.MethodGet {
		// GET /sol/slot → getSlot(finalized)
		if ltail == "" || ltail == "slot" {
			logger.Debug("sol_adapter_get_slot")
			payload := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "getSlot",
				"params":  []any{map[string]string{"commitment": "finalized"}},
			}
			return Result{
				Tail:    "",
				Method:  http.MethodPost,
				Body:    mustJSON(payload),
				Headers: ensureJSON(nil),
			}
		}
		// GET /sol/balance/{address} → getBalance(address, finalized)
		if strings.HasPrefix(ltail, "balance/") {
			addr := strings.TrimPrefix(ltail, "balance/")
			if addr != "" {
				logger.Debug("sol_adapter_get_balance", zap.String("address", addr))
				payload := map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"method":  "getBalance",
					"params": []any{
						addr,
						map[string]string{"commitment": "finalized"},
					},
				}
				return Result{
					Tail:    "",
					Method:  http.MethodPost,
					Body:    mustJSON(payload),
					Headers: ensureJSON(nil),
				}
			}
		}
		// Иной GET — как есть (даже если апстрим вернёт 405 — это честно и предсказуемо)
		return Result{
			Tail:    tail,
			Method:  method,
			Body:    nil,
			Headers: map[string]string{},
		}
	}

	// POST: прежняя логика — “разумный” дефолт и добавление commitment
	j := readJSON(body)
	if j == nil || asString(j["method"]) == "" {
		logger.Debug("sol_adapter_default_getslot")
		payload := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "getSlot",
			"params":  []any{map[string]string{"commitment": "finalized"}},
		}
		return Result{
			Tail:    "",
			Method:  "POST",
			Body:    mustJSON(payload),
			Headers: ensureJSON(nil),
		}
	}

	// Если getBalance без commitment — добавим
	if strings.EqualFold(asString(j["method"]), "getBalance") {
		if params, ok := j["params"].([]any); ok && len(params) == 1 {
			params = append(params, map[string]string{"commitment": "finalized"})
			j["params"] = params
			logger.Debug("sol_adapter_added_commitment")
		}
	}

	b, _ := json.Marshal(j)
	return Result{
		Tail:    tail,
		Method:  method,
		Body:    b,
		Headers: ensureJSON(nil),
	}
}
