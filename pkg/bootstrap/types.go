package bootstrap

import (
	"github.com/shuliakovsky/mezzium/pkg/peers"
	"go.uber.org/zap"
)

type AnnounceRequest struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	InternalAddr string `json:"internal_addr"`
	Timestamp    int64  `json:"timestamp"`
	Signature    string `json:"signature"`
}

type AnnounceResponse struct {
	Peers []peers.Peer `json:"peers"`
}

type Handler struct {
	store  *peers.Store
	myID   string
	myAddr string
	secret string
	logger *zap.Logger
}
