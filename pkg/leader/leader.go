package leader

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/peers"
	"go.uber.org/zap"
)

type heartbeat struct {
	LeaderID  string `json:"leader_id"`
	Timestamp int64  `json:"timestamp"`
}

var (
	lastHeartbeat   = make(map[string]int64)
	lastHeartbeatMu sync.RWMutex
)

func Elect(peersList []peers.Peer) string {
	if len(peersList) == 0 {
		return ""
	}
	min := peersList[0].ID
	for _, p := range peersList {
		if p.ID < min {
			min = p.ID
		}
	}
	return min
}

func RecordHeartbeat(id string, ts int64) {
	lastHeartbeatMu.Lock()
	defer lastHeartbeatMu.Unlock()
	lastHeartbeat[id] = ts
}

func IsLeaderAlive(id string, ttl time.Duration) bool {
	lastHeartbeatMu.RLock()
	defer lastHeartbeatMu.RUnlock()
	ts, ok := lastHeartbeat[id]
	if !ok {
		return false
	}
	return time.Since(time.Unix(ts, 0)) < ttl
}

func HeartbeatLoop(store *peers.Store, selfID string, ttl time.Duration, logger *zap.Logger) {
	for {
		plist := store.List()
		leaderID := Elect(plist)

		// Лидер шлёт heartbeat
		if leaderID == selfID {
			msg := heartbeat{
				LeaderID:  selfID,
				Timestamp: time.Now().Unix(),
			}
			data, _ := json.Marshal(msg)
			for _, p := range plist {
				if p.ID == selfID {
					continue
				}
				url := "http://" + p.Addr + "/heartbeat"
				_, err := http.Post(url, "application/json", bytes.NewBuffer(data))
				if err != nil {
					logger.Debug("Can't sent heartbeat", zap.String("peer", p.ID), zap.Error(err))
				}
			}
			RecordHeartbeat(selfID, msg.Timestamp)
		} else {
			// is leader alive?
			if !IsLeaderAlive(leaderID, ttl) {
				logger.Warn("leader heartbeat missed; triggering re-election", zap.String("old_leader", leaderID))
			}
		}
		time.Sleep(ttl / 2)
	}
}

func Handler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var hb heartbeat
		if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		RecordHeartbeat(hb.LeaderID, hb.Timestamp)
		logger.Debug("Heartbeat received", zap.String("leader", hb.LeaderID))
		w.WriteHeader(http.StatusOK)
	}
}
