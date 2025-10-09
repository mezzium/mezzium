package registry

import (
	"testing"

	"github.com/shuliakovsky/mezzium/pkg/networks"
	"github.com/stretchr/testify/require"
)

func TestAddAndGetNetwork(t *testing.T) {
	r := New()
	cfg := networks.NetworkConfig{
		Route:    "/testnet",
		Protocol: "evm",
		Nodes:    []networks.Node{{URL: "http://example.com", Priority: 1}},
	}
	r.AddNetwork(cfg, nil)

	all := r.All()
	require.Contains(t, all, "testnet")
	require.Equal(t, "evm", all["testnet"].Protocol)
}

func TestSanitizeNodes_MasksSecrets(t *testing.T) {
	nodes := []NodeWithPing{{
		Node: networks.Node{
			Headers: map[string]string{
				"X-Api-Key":     "secret",
				"Authorization": "Bearer token",
				"Custom":        "ok",
			},
		},
	}}
	clean := SanitizeNodes(nodes)
	h := clean[0].Headers
	require.Equal(t, "***", h["X-Api-Key"])
	require.Equal(t, "***", h["Authorization"])
	require.Equal(t, "ok", h["Custom"])
}
