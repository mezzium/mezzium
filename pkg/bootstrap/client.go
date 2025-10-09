package bootstrap

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/peers"
	"go.uber.org/zap"
)

func Announce(serverURL, id, name, internalAddr, secret string, logger *zap.Logger) ([]peers.Peer, error) {
	ts := time.Now().Unix()

	payload := id + name + internalAddr + strconv.FormatInt(ts, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))

	reqData := AnnounceRequest{
		ID:           id,
		Name:         name,
		InternalAddr: internalAddr,
		Timestamp:    ts,
		Signature:    signature,
	}

	body, _ := json.Marshal(reqData)
	resp, err := http.Post(serverURL+"/announce", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("announce request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("announce failed with status %s", resp.Status)
	}

	var announceResp AnnounceResponse
	if err := json.NewDecoder(resp.Body).Decode(&announceResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	logger.Info("announce sent", zap.String("server", serverURL))

	return announceResp.Peers, nil
}
