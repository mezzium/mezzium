package gossip

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"net/http"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/peers"
	"go.uber.org/zap"
)

func Start(store *peers.Store, selfID string, logger *zap.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		plist := store.List()
		if len(plist) <= 1 {
			continue
		}

		// Pick a random peer (excluding self)
		target := plist[rand.Intn(len(plist))]
		if target.ID == selfID {
			continue
		}

		msg := GossipMessage{From: selfID, Peers: plist}
		data, _ := json.Marshal(msg)

		url := "http://" + target.Addr + "/gossip"
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))

		if err != nil {
			store.OnFailure(target.ID)
			logger.Warn("gossip send failed", zap.String("target", target.ID), zap.Error(err))
			continue
		}
		store.OnSuccess(target.ID)
		resp.Body.Close()

		logger.Debug("Gossip sent", zap.String("to", target.ID), zap.Int("peers_count", len(plist)))
	}
}

// Inbound gossip handler
func Handler(store *peers.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var msg GossipMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Merge peer list
		for _, p := range msg.Peers {
			store.Add(p)
		}

		logger.Debug("Gossip received", zap.String("from", msg.From), zap.Int("count", len(msg.Peers)))
		w.WriteHeader(http.StatusOK)
	}
}
