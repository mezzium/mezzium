package api

import (
	"bytes"
	"context"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/shuliakovsky/mezzium/pkg/adapters"
	"github.com/shuliakovsky/mezzium/pkg/metrics"
	"github.com/shuliakovsky/mezzium/pkg/registry"
)

type Proxy struct {
	Reg      *registry.Registry
	Logger   *zap.Logger
	Client   *http.Client
	TorSocks string
}

func NewProxy(reg *registry.Registry, logger *zap.Logger, torSocks string) *Proxy {
	return &Proxy{
		Reg:      reg,
		Logger:   logger,
		Client:   &http.Client{Timeout: 8 * time.Second},
		TorSocks: torSocks,
	}
}

// Handle /{network}[/*tail]
func (p *Proxy) Serve(w http.ResponseWriter, r *http.Request) {
	// Разбор пути: /{network}/optional/tail...
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 1 {
		http.NotFound(w, r)
		return
	}
	network := parts[0]
	var tail string
	if len(parts) > 1 {
		tail = strings.Join(parts[1:], "/")
	}

	// Short-circuit: /{network}/gas → если обработано локально, выходим
	if p.TryServeGasFee(w, r, network, tail) {
		return
	}

	// Получение лучших узлов
	candidates := p.Reg.Best(network)
	if len(candidates) == 0 {
		p.Logger.Error("proxy_no_available_nodes", zap.String("network", network))
		http.Error(w, "no available nodes", http.StatusServiceUnavailable)
		return
	}

	// Чтение тела запроса
	origBody, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()

	start := LogRequest(p.Logger, "proxy", r.Method, r.URL.Path, origBody)

	// 🔧 Адаптация запроса
	protocol := p.Reg.ProtocolOf(network)
	baseURL := candidates[0].URL
	ad := adapters.Adapt(network, protocol, baseURL, tail, r.Method, r.Header, origBody, p.Logger)

	// Уважение метода: если адаптер переписал GET → POST с телом, убираем query
	rawQuery := r.URL.RawQuery
	if r.Method == http.MethodGet && ad.Method == http.MethodPost && len(ad.Body) > 0 {
		p.Logger.Debug("proxy_adapter_rewrote_get_to_post", zap.String("network", network), zap.String("tail", tail))
		rawQuery = ""
	}

	// Фильтрация по доменам, если адаптер требует
	if len(ad.AllowedHostSubstr) > 0 {
		filtered := make([]registry.NodeWithPing, 0, len(candidates))
		for _, n := range candidates {
			for _, sub := range ad.AllowedHostSubstr {
				if strings.Contains(strings.ToLower(n.URL), strings.ToLower(sub)) {
					filtered = append(filtered, n)
					break
				}
			}
		}
		candidates = filtered
		if len(candidates) == 0 {
			p.Logger.Warn("proxy_no_upstreams_match_filter", zap.String("network", network))
			http.Error(w, "no allowed upstreams", http.StatusServiceUnavailable)
			return
		}
	}

	// Подготовка заголовков
	inHeaders := r.Header.Clone()
	for k, v := range ad.Headers {
		inHeaders.Set(k, v)
	}

	// Попытки отправки запроса на upstream
	for i, node := range candidates {
		upstreamURL := buildUpstreamURL(node.URL, ad.Tail, rawQuery)

		// ⏱ Таймаут на узел
		perNodeTimeout := time.Duration(p.Reg.TimeoutMs(network)) * time.Millisecond
		if perNodeTimeout <= 0 {
			// Prefer protocol-based default to avoid slug drift
			perNodeTimeout = defaultTimeoutForProtocol(protocol)
		}

		ctx, cancel := context.WithTimeout(r.Context(), perNodeTimeout)
		req, _ := http.NewRequestWithContext(ctx, ad.Method, upstreamURL, bytes.NewReader(ad.Body))
		req.Header = inHeaders.Clone()
		for k, v := range node.Headers {
			req.Header.Set(k, v)
		}
		if req.Header.Get("content-type") == "" &&
			(ad.Method == http.MethodPost || ad.Method == http.MethodPut) {
			req.Header.Set("content-type", "application/json")
		}

		// Отправка запроса
		client := p.clientFor(node, perNodeTimeout)
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			p.Logger.Warn("proxy_upstream_error",
				zap.String("network", network),
				zap.String("upstream", upstreamURL),
				zap.Int("attempt", i+1),
				zap.Error(err),
			)
			metrics.ProxyFail.WithLabelValues(network).Inc()
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		lat := time.Since(start).Milliseconds()
		LogResponse(p.Logger, "proxy", resp.StatusCode, respBody, start)

		// Проверка на рейт-лимит или 5xx
		if isRateLimited(resp, respBody) || resp.StatusCode >= 500 {
			p.Logger.Warn("proxy_upstream_rate_or_5xx",
				zap.String("network", network),
				zap.String("upstream", upstreamURL),
				zap.Int("status", resp.StatusCode),
				zap.Int("attempt", i+1),
				zap.Int64("latency_ms", lat),
			)
			metrics.ProxyFail.WithLabelValues(network).Inc()
			continue
		}

		// ✅ Успешный ответ
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)

		p.Logger.Info("proxy_success",
			zap.String("network", network),
			zap.String("upstream", upstreamURL),
			zap.Int("status", resp.StatusCode),
			zap.Int("attempt", i+1),
			zap.Int64("latency_ms", lat),
		)
		metrics.ProxySuccess.WithLabelValues(network).Inc()
		return
	}

	// Все попытки исчерпаны
	p.Logger.Error("proxy_all_upstreams_failed", zap.String("network", network))
	http.Error(w, "all upstreams failed", http.StatusBadGateway)
	metrics.ProxyFail.WithLabelValues(network).Inc()
}

func (p *Proxy) clientFor(node registry.NodeWithPing, timeout time.Duration) *http.Client {
	tr := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 8 * time.Second,
	}
	if node.Tor {
		dialer, err := proxy.SOCKS5("tcp", p.TorSocks, nil, proxy.Direct)
		if err == nil {
			tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			}
		} else {
			p.Logger.Warn("tor_socks5_dialer_error", zap.Error(err))
		}
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

func buildUpstreamURL(base, tail, rawQuery string) string {
	u := strings.TrimRight(base, "/")
	if tail != "" {
		u += "/" + strings.TrimLeft(tail, "/")
	}
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	return u
}

func defaultTimeoutForProtocol(protocol string) time.Duration {
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
