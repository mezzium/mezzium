package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/registry"
)

type EIP1559 struct {
	BaseFeeGwei     int64
	PriorityFeeGwei int64
	BlockNumber     int64
}

type EVMClient struct {
	Timeout time.Duration
}

func NewEVMClient(timeout time.Duration) *EVMClient {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &EVMClient{Timeout: timeout}
}

func (c *EVMClient) Compute(node registry.NodeWithPing) (EIP1559, error) {
	// base fee
	baseFeeGwei, err := c.baseFeeFromHistory(node)
	if err != nil {
		return EIP1559{}, err
	}
	// priority fee
	prioGwei, err := c.priorityFee(node)
	if err != nil {
		return EIP1559{}, err
	}
	// block number
	block, _ := c.blockNumber(node)

	return EIP1559{
		BaseFeeGwei:     baseFeeGwei,
		PriorityFeeGwei: prioGwei,
		BlockNumber:     block,
	}, nil
}

func (c *EVMClient) baseFeeFromHistory(node registry.NodeWithPing) (int64, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_feeHistory", "params": []any{"0x10", "latest", []any{}}}
	var out struct {
		Result struct {
			BaseFeePerGas []string `json:"baseFeePerGas"`
		} `json:"result"`
	}
	if err := c.rpc(node, req, &out); err != nil {
		return 0, err
	}
	if len(out.Result.BaseFeePerGas) == 0 {
		return 0, simpleErr("empty baseFeePerGas")
	}
	return hexWeiToGwei(out.Result.BaseFeePerGas[len(out.Result.BaseFeePerGas)-1]), nil
}

func (c *EVMClient) priorityFee(node registry.NodeWithPing) (int64, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "eth_maxPriorityFeePerGas", "params": []any{}}
	var out struct {
		Result string `json:"result"`
	}
	if err := c.rpc(node, req, &out); err != nil {
		return 0, err
	}
	if out.Result == "" {
		return 0, simpleErr("empty priorityFee")
	}
	return hexWeiToGwei(out.Result), nil
}

func (c *EVMClient) blockNumber(node registry.NodeWithPing) (int64, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": 3, "method": "eth_blockNumber", "params": []any{}}
	var out struct {
		Result string `json:"result"`
	}
	if err := c.rpc(node, req, &out); err != nil {
		return 0, err
	}
	return hexToInt64(out.Result), nil
}

func (c *EVMClient) rpc(node registry.NodeWithPing, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, node.URL, bytes.NewReader(b))
	for k, v := range node.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("content-type", "application/json")

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return simpleErr("rpc non-2xx")
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e simpleErr) Error() string { return string(e) }

func hexWeiToGwei(h string) int64 {
	if h == "" {
		return 0
	}
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(strings.ToLower(h), "0x"), 16)
	return new(big.Int).Div(n, big.NewInt(1_000_000_000)).Int64()
}
func hexToInt64(h string) int64 {
	if h == "" {
		return 0
	}
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(strings.ToLower(h), "0x"), 16)
	if n.IsInt64() {
		return n.Int64()
	}
	return new(big.Int).SetUint64(n.Uint64()).Int64()
}
