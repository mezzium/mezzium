package adapters

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

type Result struct {
	Tail              string            // tail after /{network}
	Method            string            // final HTTP-method
	Body              []byte            // final body
	Headers           map[string]string // headers-overrides
	AllowedHostSubstr []string          // If specified, the proxy forwards requests only to upstreams whose URLs contain at least one of the defined substrings.
}

func Adapt(network, protocol, baseURL, tail, method string, hdr http.Header, body []byte, logger *zap.Logger) Result {
	switch strings.ToLower(network) {
	case "trx":
		return adaptTRX(tail, method, hdr, body, logger)
	case "btc":
		return adaptBTC(tail, method, hdr, body, logger)
	case "nft":
		return adaptNFT(tail, method, hdr, body, logger)
	case "sol":
		return adaptSOL(tail, method, hdr, body, logger)
	case "doge":
		return adaptDOGE(tail, method, hdr, body, logger, baseURL)
	case "ltc":
		return adaptLTC(tail, method, hdr, body, logger, baseURL)
	default:
		// default behaviour
		if strings.EqualFold(protocol, "evm") {
			return adaptEVM(tail, method, hdr, body, logger)
		}
		return Result{
			Tail:    tail,
			Method:  method,
			Body:    clone(body),
			Headers: map[string]string{},
		}
	}
}

func clone(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

func ensureJSON(h map[string]string) map[string]string {
	if h == nil {
		h = map[string]string{}
	}
	if _, ok := h["Content-Type"]; !ok {
		h["Content-Type"] = "application/json"
	}
	return h
}

func readJSON(body []byte) map[string]any {
	if len(body) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		return m
	}
	return nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func readAllAndClose(r io.ReadCloser) []byte {
	if r == nil {
		return nil
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	return b
}

func bytesReader(b []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(b))
}
