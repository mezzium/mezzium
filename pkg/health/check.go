package health

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/networks"
	"github.com/shuliakovsky/mezzium/pkg/registry"
	"github.com/shuliakovsky/mezzium/pkg/secrets"
	"go.uber.org/zap"
	"golang.org/x/net/proxy"
)

type Checker struct {
	TorSocks5 string
	Logger    *zap.Logger
	Reg       *registry.Registry
	dropMu    sync.Mutex
	dropURLs  map[string]struct{}
}

func New(tor string, logger *zap.Logger, reg *registry.Registry) *Checker {
	if tor == "" {
		tor = "127.0.0.1:9050"
	}
	return &Checker{
		TorSocks5: tor,
		Logger:    logger,
		Reg:       reg,
		dropURLs:  map[string]struct{}{},
	}
}

func (c *Checker) httpClient(tor bool, timeout time.Duration) (*http.Client, error) {
	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 8 * time.Second,
	}
	if tor {
		dialer, err := proxy.SOCKS5("tcp", c.TorSocks5, nil, proxy.Direct)
		if err != nil {
			return nil, err
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

func (c *Checker) perNodeTimeout(protocol string) time.Duration {
	tmo := time.Duration(c.Reg.TimeoutMs(protocol)) * time.Millisecond
	if tmo <= 0 {
		tmo = defaultTimeoutFor(protocol)
	}
	return tmo
}

func defaultTimeoutFor(protocol string) time.Duration {
	switch strings.ToLower(protocol) {
	case "sol":
		return 800 * time.Millisecond
	case "evm":
		return 1500 * time.Millisecond
	case "trx":
		return 1500 * time.Millisecond
	case "btc", "doge", "ltc":
		return 2000 * time.Millisecond
	default:
		return 1500 * time.Millisecond
	}
}

// === UpdateNetwork ===
func (c *Checker) UpdateNetwork(protocol string, nodes []networks.Node) []registry.NodeWithPing {
	res := make([]registry.NodeWithPing, 0, len(nodes))
	// get timeout for this network
	tmo := c.perNodeTimeout(protocol)

	for _, n := range nodes {
		var alive bool
		var ping int64
		switch protocol {
		case "evm":
			alive, ping = c.checkEVM(n, tmo)
		case "btc":
			alive, ping = c.checkBTC(n, tmo)
		case "trx":
			alive, ping = c.checkTRX(n, tmo)
		case "ltc":
			alive, ping = c.checkLTC(n, tmo)
		case "doge":
			alive, ping = c.checkDOGE(n, tmo)
		case "sol":
			alive, ping = c.checkSOL(n, tmo)
		default:
			alive, ping = false, 0
		}
		safeHeaders := secrets.RedactHeaders(n.Headers)

		if !alive {
			c.Logger.Warn("health_node_unhealthy",
				zap.String("url", secrets.RedactString(n.URL)),
				zap.String("protocol", protocol),
				zap.Int("priority", n.Priority),
				zap.Any("headers", safeHeaders),
			)
		} else {
			c.Logger.Debug("health_node_alive",
				zap.String("url", secrets.RedactString(n.URL)),
				zap.String("protocol", protocol),
				zap.Int("priority", n.Priority),
				zap.Int64("ping_ms", ping),
				zap.Any("headers", safeHeaders),
			)
		}
		res = append(res, registry.NodeWithPing{Node: n, Alive: alive, Ping: ping})
	}
	return registry.PickFastestPerPriority(res)
}

func safeURLField(url string) zap.Field {
	return zap.String("url", secrets.RedactString(url))
}

func safeHeadersField(h map[string]string) zap.Field {
	return zap.Any("headers", secrets.RedactHeaders(h))
}

// treat 4xx (except 429) as fatal
func isFatalHTTPStatus(code int) bool {
	if code == 0 {
		return false
	}
	return code >= 400 && code < 500 && code != 429
}

// classify network-level errors considered fatal
func isFatalNetErr(err error) bool {
	if err == nil {
		return false
	}
	// DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	// connection refused
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil && errors.Is(opErr.Err, syscall.ECONNREFUSED) {
		return true
	}
	// unsupported protocol / bad URL
	if strings.Contains(err.Error(), "unsupported protocol scheme") {
		return true
	}
	if strings.Contains(strings.ToLower(err.Error()), "no such host") {
		return true
	}
	// TLS/x509 issues
	if strings.Contains(strings.ToLower(err.Error()), "x509:") ||
		strings.Contains(strings.ToLower(err.Error()), "tls") {
		return true
	}
	return false
}

func (c *Checker) markDrop(url string) {
	c.dropMu.Lock()
	c.dropURLs[url] = struct{}{}
	c.dropMu.Unlock()
}

// DrainDropURLs returns and clears accumulated URLs to drop.
func (c *Checker) DrainDropURLs() []string {
	c.dropMu.Lock()
	defer c.dropMu.Unlock()
	out := make([]string, 0, len(c.dropURLs))
	for u := range c.dropURLs {
		out = append(out, u)
	}
	c.dropURLs = map[string]struct{}{}
	return out
}
