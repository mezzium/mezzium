package adapters

import (
	"go.uber.org/zap"
	"net/http"
	"strings"
)

func adaptLTC(tail, method string, _ http.Header, body []byte, logger *zap.Logger, baseURL string) Result {
	ltail := strings.ToLower(strings.TrimPrefix(tail, "/"))
	lbase := strings.ToLower(baseURL)

	// 1) Tatum JSON-RPC
	if strings.Contains(lbase, "tatum.io") {
		logger.Debug("ltc_adapter_tatum")
		return Result{
			Tail:    "",
			Method:  http.MethodPost,
			Body:    []byte(`{"jsonrpc":"2.0","method":"getblockcount","params":[],"id":1}`),
			Headers: ensureJSON(nil), // x-api-key уже в n.Headers
		}
	}

	// 2) Прямые REST/API вызовы — прокидываем как есть
	if strings.HasPrefix(ltail, "rest/") || strings.HasPrefix(ltail, "api/") {
		return Result{
			Tail:    tail,
			Method:  method,
			Body:    clone(body),
			Headers: map[string]string{},
		}
	}

	// 3) Нет хвоста — дефолт под тип апстрима
	if ltail == "" {
		logger.Debug("ltc_adapter_default_height")
		switch {
		case strings.Contains(lbase, "sochain.com"):
			// SoChain API
			return Result{
				Tail:    "api/v2/get_info/LTC",
				Method:  http.MethodGet,
				Body:    nil,
				Headers: map[string]string{},
			}
		case strings.Contains(lbase, "blockbook") || strings.Contains(lbase, "blockchair"):
			// Blockbook API
			return Result{
				Tail:    "api/v2",
				Method:  http.MethodGet,
				Body:    nil,
				Headers: map[string]string{},
			}
		default:
			// Litecoin Core REST
			return Result{
				Tail:    "rest/chaininfo.json",
				Method:  http.MethodGet,
				Body:    nil,
				Headers: map[string]string{},
			}
		}
	}

	// 4) Всё остальное — JSON-RPC
	return Result{
		Tail:    tail,
		Method:  method,
		Body:    clone(body),
		Headers: ensureJSON(nil),
	}
}
