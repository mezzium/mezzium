package health

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/networks"
)

// === BTC ===
func (c *Checker) checkBTC(n networks.Node, timeout time.Duration) (bool, int64) {
	cl, _ := c.httpClient(n.Tor, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	u := strings.TrimSuffix(n.URL, "/")

	// Blockstream REST
	if strings.Contains(u, "blockstream.info") || strings.HasSuffix(u, "/api") {
		req, _ := http.NewRequestWithContext(ctx, "GET", u+"/blocks/tip/height", nil)
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
		io.Copy(io.Discard, resp.Body)
		return true, time.Since(start).Milliseconds()
	}

	// Tatum gateway JSON-RPC
	if strings.Contains(u, "gateway.tatum.io") {
		payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"getblockcount","params":[]}`)
		req, _ := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(payload))
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
		io.Copy(io.Discard, resp.Body)
		return true, time.Since(start).Milliseconds()
	}

	return false, 0
}
