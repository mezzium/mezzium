package health

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/networks"
)

// === SOL ===
func (c *Checker) checkSOL(n networks.Node, timeout time.Duration) (bool, int64) {
	cl, _ := c.httpClient(n.Tor, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	body := map[string]any{"jsonrpc": "2.0", "method": "getSlot", "id": 1}
	js, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", n.URL, bytes.NewReader(js))
	for k, v := range n.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := cl.Do(req)
	if err != nil {
		if isFatalNetErr(err) {
			c.markDrop(n.URL)
		}
		return false, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isFatalHTTPStatus(resp.StatusCode) {
			c.markDrop(n.URL)
		}
		return false, 0
	}

	return true, time.Since(start).Milliseconds()
}
