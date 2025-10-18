package adapters

import (
	"encoding/hex"
	"math/big"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// Virtual network "nft":
// - Expect query parameters: address (contract) and tokenId
// - Convert to eth_call(ownerOf(uint256)) via ABI
// - If full JSON-RPC is already provided — do not interfere
//
// Requirement: "nft" config must point to EVM RPC
func adaptNFT(tail, method string, _ http.Header, body []byte, logger *zap.Logger) Result {
	// in case of request is  JSON-RPC — as is
	if m := readJSON(body); m != nil && strings.EqualFold(asString(m["method"]), "eth_call") {
		return Result{
			Tail:    tail,
			Method:  method,
			Body:    clone(body),
			Headers: ensureJSON(nil),
		}
	}

	// /nft/{address}/{tokenId}
	parts := filterEmpty(strings.Split(tail, "/"))
	if len(parts) >= 2 && isHexAddress(parts[0]) {
		contract := parts[0]
		tokenId := parts[1]

		data := BuildOwnerOfData(tokenId)
		payload := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "eth_call",
			"params": []any{
				map[string]string{
					"to":   NormalizeHex(contract),
					"data": data,
				},
				"latest",
			},
		}
		logger.Debug("nft_adapter_ownerOf",
			zap.String("contract", contract),
			zap.String("tokenId", tokenId),
		)
		return Result{
			Tail:    "", // на EVM RPC корень
			Method:  "POST",
			Body:    mustJSON(payload),
			Headers: ensureJSON(nil),
		}
	}

	return Result{
		Tail:    tail,
		Method:  method,
		Body:    clone(body),
		Headers: ensureJSON(nil),
	}
}

func isHexAddress(s string) bool {
	s = strings.ToLower(s)
	if strings.HasPrefix(s, "0x") {
		s = s[2:]
	}
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// BuildOwnerOfData: ownerOf(uint256) → 0x6352211e + tokenId 32-byte left-padded
func BuildOwnerOfData(tokenId string) string {
	selector := "0x6352211e"

	// Parse tokenId as decimal or hex (с 0x)
	id := new(big.Int)
	if strings.HasPrefix(strings.ToLower(tokenId), "0x") {
		id.SetString(tokenId[2:], 16)
	} else {
		id.SetString(tokenId, 10)
	}

	enc := leftPad32(id.Bytes())
	return selector + hex.EncodeToString(enc)
}

func leftPad32(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func filterEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
func mustHex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}
func NormalizeHex(s string) string {
	if strings.HasPrefix(s, "0x") {
		return s
	}
	return "0x" + s
}

// BuildTokenURIData: tokenURI(uint256) → 0xc87b56dd + tokenId 32-byte left-padded
func BuildTokenURIData(tokenId string) string {
	selector := "0xc87b56dd"
	id := new(big.Int)
	if strings.HasPrefix(strings.ToLower(tokenId), "0x") {
		id.SetString(tokenId[2:], 16)
	} else {
		id.SetString(tokenId, 10)
	}
	enc := leftPad32(id.Bytes())
	return selector + hex.EncodeToString(enc)
}

// BuildBalanceOfData: balanceOf(address) → 0x70a08231 + 32-byte left-padded address
func BuildBalanceOfData(addr string) string {
	selector := "0x70a08231"
	a := strings.ToLower(addr)
	if strings.HasPrefix(a, "0x") {
		a = a[2:]
	}
	addrBytes, _ := hex.DecodeString(a)
	enc := leftPad32(addrBytes)
	return selector + hex.EncodeToString(enc)
}

// BuildTokenOfOwnerByIndexData: tokenOfOwnerByIndex(address,uint256) → 0x2f745c59 + encoded args
func BuildTokenOfOwnerByIndexData(owner string, index uint64) string {
	selector := "0x2f745c59"
	// ABI: two 32-byte words, owner (left-padded), index (left-padded)
	o := strings.ToLower(owner)
	if strings.HasPrefix(o, "0x") {
		o = o[2:]
	}
	ownerBytes, _ := hex.DecodeString(o)
	indexBI := new(big.Int).SetUint64(index)
	data := append(leftPad32(ownerBytes), leftPad32(indexBI.Bytes())...)
	return selector + hex.EncodeToString(data)
}

// BuildSupportsInterfaceData: supportsInterface(bytes4) → 0x01ffc9a7 + 32-byte left-padded interfaceId
func BuildSupportsInterfaceData(ifaceHex4 string) string {
	selector := "0x01ffc9a7"
	id := strings.TrimPrefix(strings.ToLower(ifaceHex4), "0x")
	padded := leftPad32(mustHex(id))
	return selector + hex.EncodeToString(padded)
}
