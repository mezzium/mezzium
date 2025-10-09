package gossip

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/networks"
	"github.com/shuliakovsky/mezzium/pkg/peers"
	"github.com/shuliakovsky/mezzium/pkg/registry"
	"go.uber.org/zap"
)

func Publisher(reg *registry.Registry, peersStore *peers.Store, selfID string, logger *zap.Logger) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		plist := peersStore.List()
		if len(plist) <= 1 {
			continue
		}
		peer := plist[int(time.Now().UnixNano())%len(plist)]
		msg := buildAdvert(selfID, reg)
		data, _ := json.Marshal(msg)
		_, err := http.Post("http://"+peer.Addr+"/gossip-state", "application/json", bytes.NewReader(data))
		if err != nil {
			logger.Debug("gossip_state_send_error", zap.String("peer", peer.ID), zap.Error(err))
			continue
		}
		logger.Debug("gossip_state_sent", zap.String("to", peer.ID), zap.Int("networks", len(msg.Networks)))
	}
}

func buildAdvert(selfID string, reg *registry.Registry) StateMessage {
	all := reg.All()
	nets := make([]NetworkAdvert, 0, len(all))
	for name, st := range all {
		var nodes []NodeAdvert
		for _, n := range st.Best {
			nodes = append(nodes, NodeAdvert{
				URL:      n.URL, // do not distribute sensitive headers
				Priority: n.Priority,
				Protocol: st.Protocol,
				Alive:    n.Alive,
				Ping:     n.Ping,
			})
		}
		nets = append(nets, NetworkAdvert{
			Name:     name,
			Protocol: st.Protocol,
			Nodes:    nodes,
			Ts:       time.Now().Unix(),
		})
	}
	return StateMessage{From: selfID, Networks: nets}
}

func StateHandler(reg *registry.Registry, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var msg StateMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Soft-merge: add new URLs into All; health loop will validate them
		all := reg.All()
		for _, adv := range msg.Networks {
			if st, ok := all[adv.Name]; ok {
				exists := map[string]struct{}{}
				for _, n := range st.All {
					exists[n.URL] = struct{}{}
				}
				for _, n := range adv.Nodes {
					if _, ok := exists[n.URL]; !ok {

						ttl := 10 * time.Minute
						maxDiscovered := 20
						for _, n := range adv.Nodes {
							if _, ok := exists[n.URL]; ok {
								continue
							}
							if len(st.Discovered) >= maxDiscovered {
								continue
							}
							st.Discovered = append(st.Discovered, registry.DiscoveredNode{
								Node:      networks.Node{URL: n.URL, Priority: n.Priority, Headers: map[string]string{}},
								ExpiresAt: time.Now().Add(ttl),
							})
						}
					}
				}
			}
		}
		logger.Debug("gossip_state_received", zap.String("from", msg.From), zap.Int("networks", len(msg.Networks)))
		w.WriteHeader(http.StatusOK)
	}
}
