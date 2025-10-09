package health

import (
	"bytes"
	"context"
	"encoding/json"
	"go.uber.org/zap"
	"net/http"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/networks"
)

// === EVM ===
func (c *Checker) checkEVM(n networks.Node, timeout time.Duration) (bool, int64) {
	cl, _ := c.httpClient(n.Tor, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	body := map[string]any{"jsonrpc": "2.0", "method": "eth_blockNumber", "id": 1}
	js, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", n.URL, bytes.NewReader(js))
	for k, v := range n.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("content-type", "application/json")

	start := time.Now()
	resp, err := cl.Do(req)
	if err != nil {
		if isFatalNetErr(err) {
			c.markDrop(n.URL)
		}
		c.Logger.Warn("evm_health_request_error",
			safeURLField(n.URL),
			safeHeadersField(n.Headers),
			zap.Error(err),
		)
		return false, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isFatalHTTPStatus(resp.StatusCode) {
			c.markDrop(n.URL)
		}
		c.Logger.Warn("evm_health_bad_status",
			safeURLField(n.URL),
			safeHeadersField(n.Headers),
			zap.Int("status", resp.StatusCode),
		)
		return false, 0
	}

	var out struct {
		Result string          `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		c.Logger.Warn("evm_health_decode_error",
			safeURLField(n.URL),
			safeHeadersField(n.Headers),
			zap.Error(err),
		)

		return false, 0
	}

	if out.Result == "" {
		if len(out.Error) > 0 {
			c.Logger.Warn("evm_health_rpc_error",
				safeURLField(n.URL),
				safeHeadersField(n.Headers),
				zap.ByteString("error", out.Error),
			)

		} else {
			c.Logger.Warn("evm_health_empty_result",
				safeURLField(n.URL),
				safeHeadersField(n.Headers),
			)

		}
		return false, 0
	}

	return true, time.Since(start).Milliseconds()
}
