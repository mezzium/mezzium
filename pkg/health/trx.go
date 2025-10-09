package health

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/networks"
)

// === TRX ===
func (c *Checker) checkTRX(n networks.Node, timeout time.Duration) (bool, int64) {
	cl, _ := c.httpClient(n.Tor, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	u := strings.TrimSuffix(n.URL, "/")

	// === Tatum REST ===
	if strings.Contains(n.URL, "tatum.io") {
		req, _ := http.NewRequestWithContext(ctx, "GET", u+"/wallet/getnodeinfo", nil)
		for k, v := range n.Headers {
			req.Header.Set(k, v)
		}
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

	// === TronGrid / обычный FullNode API ===
	req, _ := http.NewRequestWithContext(ctx, "POST", u+"/wallet/getnowblock", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range n.Headers {
		req.Header.Set(k, v)
	}
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
