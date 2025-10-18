package main

import (
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

	// local mux so we can apply global middleware (CORS) once
	mux := http.NewServeMux()

	// Core control endpoints
	mux.Handle("/announce", bootstrap.NewHandler(peerStore, nodeID, internalAddr, cfg.SharedSecret, logger))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/gossip", gossip.Handler(peerStore, logger))
	mux.HandleFunc("/heartbeat", leader.Handler(logger))

	// Swagger
	mux.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/swagger.json"),
		httpSwagger.InstanceName("swagger"),
	))
	mux.HandleFunc("/swagger/swagger.json", docs.JSONHandler)

	// Gossip state exchange
	mux.HandleFunc("/gossip-state", gossip.StateHandler(reg, logger))
	go gossip.Publisher(reg, peerStore, nodeID, logger)

	// Public routes
	mux.HandleFunc("/active-nodes", public.ActiveNodes)

	// Bitcoin helpers
	mux.HandleFunc("/btc/balance/", public.BTCBalance)

	// NFT helpers
	mux.HandleFunc("/ethereum/nft/get-all-nfts/", public.NFTGetAllNFTs)
	mux.HandleFunc("/ethereum/nft/get-nft-metadata/", public.NFTGetNFTMetadata)

	// Admin routes
	mux.HandleFunc("/admin/networks", adminAPI.AddNetwork)
	mux.HandleFunc("/admin/networks/bulk", adminAPI.AddNetworksBulk)
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/ws/", wsAPI.ServeWS)

	// Dynamic proxy routes from registry
	for name, st := range reg.All() {
		route := st.Route
		logger.Info("route_registered", zap.String("network", name), zap.String("route", route))
		mux.HandleFunc(route, proxy.Serve)
		if !strings.HasSuffix(route, "/") {
			mux.HandleFunc(route+"/", proxy.Serve)
		}
	}

	// Metrics (register handler on mux)
	metrics.Init()
	mux.Handle("/metrics", metrics.Handler())

	// CORS wrapper applied once to the entire mux
	corsWrapper := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Tell browsers we allow cross-origin requests
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Accept,Authorization,x-api-key,x-admin-key,X-Requested-With")
			// small prudent defaults
			w.Header().Set("Access-Control-Max-Age", "600")

			// Handle preflight fast
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			h.ServeHTTP(w, r)
		})
	}

	// Register wrapped mux as the global handler
	http.Handle("/", corsWrapper(mux))
}
