package main

import (
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/shuliakovsky/mezzium/pkg/bootstrap"
	"github.com/shuliakovsky/mezzium/pkg/peers"
)

func initBootstrap(cfg config, logger *zap.Logger) (*peers.Store, string, string) {
	nodeID := uuid.NewString()
	internalAddr := fmt.Sprintf("%s:%s", cfg.PodIP, cfg.Port)

	logger.Info("Node started",
		zap.String("podName", cfg.PodName),
		zap.String("nodeID", nodeID),
		zap.String("internalAddr", internalAddr),
	)

	peerStore := peers.NewStore()
	peerStore.Add(peers.Peer{ID: nodeID, Addr: internalAddr})

	if cfg.BootstrapURL != "" {
		if list, err := bootstrap.Announce(cfg.BootstrapURL, nodeID, cfg.PodName, internalAddr, cfg.SharedSecret, logger); err != nil {
			logger.Warn("Boostrap error", zap.Error(err))
		} else {
			for _, p := range list {
				if p.ID != nodeID {
					peerStore.Add(p)
				}
			}
		}
	}

	return peerStore, nodeID, internalAddr
}
