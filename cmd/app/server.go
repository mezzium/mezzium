package main

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"
)

func startServer(host, port string, logger *zap.Logger) {
	addr := fmt.Sprintf("%s:%s", host, port)
	logger.Info("Listening", zap.String("addr", addr))
	handler := withCORS(http.DefaultServeMux)
	if err := http.ListenAndServe(addr, handler); err != nil && err != http.ErrServerClosed {
		logger.Fatal("Server down", zap.Error(err))
	}
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-admin-key, x-rpc-switch")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		h.ServeHTTP(w, r)
	})
}
