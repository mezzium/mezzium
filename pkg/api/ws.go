package api

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/shuliakovsky/mezzium/pkg/metrics"
	"github.com/shuliakovsky/mezzium/pkg/registry"
	"github.com/shuliakovsky/mezzium/pkg/secrets"
	"go.uber.org/zap"
)

type WS struct {
	Reg    *registry.Registry
	Logger *zap.Logger
}

func NewWS(reg *registry.Registry, logger *zap.Logger) *WS {
	return &WS{Reg: reg, Logger: logger}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (w *WS) ServeWS(rw http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/ws/"), "/")
	if len(parts) < 1 {
		http.Error(rw, "bad path", http.StatusBadRequest)
		return
	}
	network := parts[0]
	nodes := w.Reg.Best(network)
	if len(nodes) == 0 {
		http.Error(rw, "no healthy nodes", http.StatusServiceUnavailable)
		return
	}
	upstream := nodes[0].URL
	if !strings.HasPrefix(upstream, "ws") {
		http.Error(rw, "upstream is not websocket", http.StatusBadGateway)
		return
	}

	clientConn, err := upgrader.Upgrade(rw, r, nil)
	if err != nil {
		w.Logger.Warn("ws_upgrade_failed", zap.Error(err))
		metrics.WSError.WithLabelValues(network).Inc()
		return
	}
	defer clientConn.Close()

	upstreamConn, _, err := websocket.DefaultDialer.Dial(upstream, nil)
	if err != nil {
		w.Logger.Warn("ws_dial_failed", zap.String("upstream", upstream), zap.Error(err))
		metrics.WSError.WithLabelValues(network).Inc()
		return
	}
	defer upstreamConn.Close()

	w.Logger.Info("ws_proxy_connected", zap.String("network", network), zap.String("upstream", upstream))
	metrics.WSConnected.WithLabelValues(network).Inc()

	// bidirectional copy
	go func() {
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				w.Logger.Warn("ws_client_read_error", zap.Error(err))
				metrics.WSError.WithLabelValues(network).Inc()
				return
			}
			if mt == websocket.TextMessage {
				if strings.Contains(string(msg), "eth_subscribe") {
					w.Logger.Info("ws_subscribe", zap.String("network", network), zap.String("payload", secrets.RedactString(string(msg))))
				}
				if strings.Contains(string(msg), "eth_unsubscribe") {
					w.Logger.Info("ws_unsubscribe", zap.String("network", network), zap.String("payload", secrets.RedactString(string(msg))))
				}
			}
			if err := upstreamConn.WriteMessage(mt, msg); err != nil {
				w.Logger.Warn("ws_upstream_write_error", zap.Error(err))
				metrics.WSError.WithLabelValues(network).Inc()
				return
			}
		}
	}()

	for {
		mt, msg, err := upstreamConn.ReadMessage()
		if err != nil {
			w.Logger.Warn("ws_upstream_read_error", zap.Error(err))
			metrics.WSError.WithLabelValues(network).Inc()
			return
		}
		if err := clientConn.WriteMessage(mt, msg); err != nil {
			w.Logger.Warn("ws_client_write_error", zap.Error(err))
			metrics.WSError.WithLabelValues(network).Inc()
			return
		}
	}
}
