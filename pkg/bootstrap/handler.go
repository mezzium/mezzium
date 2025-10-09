package bootstrap

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/shuliakovsky/mezzium/pkg/peers"
	"go.uber.org/zap"
)

func NewHandler(store *peers.Store, myID, myAddr, secret string, logger *zap.Logger) *Handler {
	return &Handler{
		store:  store,
		myID:   myID,
		myAddr: myAddr,
		secret: secret,
		logger: logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AnnounceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	payload := req.ID + req.Name + req.InternalAddr + strconv.FormatInt(req.Timestamp, 10)
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(req.Signature)) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}

	h.store.Add(peers.Peer{
		ID:   req.ID,
		Addr: req.InternalAddr,
	})

	peersList := h.store.List()
	peersList = append(peersList, peers.Peer{ID: h.myID, Addr: h.myAddr})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AnnounceResponse{Peers: peersList})

	h.logger.Info("New peer has been added", zap.String("peerID", req.ID), zap.String("addr", req.InternalAddr))
}
