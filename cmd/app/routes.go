package main

import (
	"encoding/json"

	"net/http"
	"strings"

	"go.uber.org/zap"

	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/shuliakovsky/mezzium/pkg/api"
	"github.com/shuliakovsky/mezzium/pkg/bootstrap"
	"github.com/shuliakovsky/mezzium/pkg/docs"
	_ "github.com/shuliakovsky/mezzium/pkg/docs"
	"github.com/shuliakovsky/mezzium/pkg/gossip"
	"github.com/shuliakovsky/mezzium/pkg/health"
	"github.com/shuliakovsky/mezzium/pkg/leader"
	"github.com/shuliakovsky/mezzium/pkg/metrics"
	"github.com/shuliakovsky/mezzium/pkg/peers"
	"github.com/shuliakovsky/mezzium/pkg/registry"
	"github.com/shuliakovsky/mezzium/pkg/secrets"
)

func registerRoutes(
	reg *registry.Registry,
	checker *health.Checker,
	peerStore *peers.Store,
	nodeID string,
	internalAddr string,
	cfg config,
	logger *zap.Logger,
) {
	public := api.NewPublic(reg, logger)
	proxy := api.NewProxy(reg, logger, cfg.TorSocks)
	adminAPI := api.NewAdmin(reg, checker, cfg.AdminKey, logger)
	wsAPI := api.NewWS(reg, logger)

	// Core control endpoints
	http.Handle("/announce", bootstrap.NewHandler(peerStore, nodeID, internalAddr, cfg.SharedSecret, logger))
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	http.HandleFunc("/gossip", gossip.Handler(peerStore, logger))
	http.HandleFunc("/heartbeat", leader.Handler(logger))

	// Swagger
	http.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/swagger.json"),
		httpSwagger.InstanceName("swagger"),
	))
	http.HandleFunc("/swagger/swagger.json", docs.JSONHandler)

	// Gossip state exchange
	http.HandleFunc("/gossip-state", gossip.StateHandler(reg, logger))
	go gossip.Publisher(reg, peerStore, nodeID, logger)

	// Public routes
	http.HandleFunc("/networkfees", public.NetworkFees)
	http.HandleFunc("/active-nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		type liteNode struct {
			URL      string `json:"url"`
			Priority int    `json:"priority"`
		}
		out := make(map[string][]liteNode)
		for name, st := range reg.All() {
			if len(st.Best) == 0 {
				out[name] = []liteNode{}
				continue
			}
			arr := make([]liteNode, 0, len(st.Best))
			for _, n := range st.Best {
				// Маскируем ключи из env (x-api-key и т.п.)
				masked := n.URL
				masked = secrets.RedactString(masked)
				arr = append(arr, liteNode{
					URL:      masked,
					Priority: n.Priority,
				})
			}
			out[name] = arr
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	// Fee helpers
	http.HandleFunc("/proxy/eth/fee", public.EthFee)
	http.HandleFunc("/proxy/eth/maxPriorityFee", public.EthMaxPriorityFee)
	http.HandleFunc("/proxy/btc/fees", public.BTCFees)
	http.HandleFunc("/proxy/btc/balance/", public.BTCBalance)

	// NFT helpers
	http.HandleFunc("/proxy/nft/get-all-nfts/", public.NFTGetAllNFTs)
	http.HandleFunc("/proxy/nft/get-nft-metadata/", public.NFTGetNFTMetadata)
	http.HandleFunc("/proxy/eth/estimateGas", public.EthEstimateGas)

	// Admin routes
	http.HandleFunc("/admin/networks", adminAPI.AddNetwork)
	http.HandleFunc("/admin/networks/bulk", adminAPI.AddNetworksBulk)
	http.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/nodes") && r.Method == http.MethodGet:
			adminAPI.ListNodes(w, r)
		case strings.HasSuffix(r.URL.Path, "/nodes") && r.Method == http.MethodDelete:
			adminAPI.DeleteNode(w, r)
		case strings.HasSuffix(r.URL.Path, "/nodes") && r.Method == http.MethodPost:
			adminAPI.AddNode(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// WebSocket
	http.HandleFunc("/ws/", wsAPI.ServeWS)

	// Dynamic proxy routes from registry
	for name, st := range reg.All() {
		route := st.Route
		logger.Info("route_registered", zap.String("network", name), zap.String("route", route))
		http.HandleFunc(route, proxy.Serve)
		if !strings.HasSuffix(route, "/") {
			http.HandleFunc(route+"/", proxy.Serve)
		}
	}

	// Metrics
	metrics.Init()
	http.Handle("/metrics", metrics.Handler())
}
