package health

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/networks"
)

// === DOGE ===
func (c *Checker) checkDOGE(n networks.Node, timeout time.Duration) (bool, int64) {
	cl, _ := c.httpClient(n.Tor, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	u := strings.TrimSuffix(n.URL, "/")

	// === Tatum JSON-RPC ===
	if strings.Contains(n.URL, "tatum.io") {
		body := `{"jsonrpc":"2.0","method":"getblockcount","params":[],"id":1}`
		req, _ := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(body))
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

	// === Dogecoin Core REST ===
	req, _ := http.NewRequestWithContext(ctx, "GET", u+"/rest/chaininfo.json", nil)
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
