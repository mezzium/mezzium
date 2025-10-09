package registry

import (
	"sync"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/networks"
)

type Registry struct {
	mu    sync.RWMutex
	State map[string]*NetworkState // key: network name (eth, btc)
}

type NodeWithPing struct {
	networks.Node
	Alive bool  `json:"alive"`
	Ping  int64 `json:"ping"` // ms
}

type DiscoveredNode struct {
	Node      networks.Node
	ExpiresAt time.Time
}

type NetworkState struct {
	Protocol   string
	Route      string
	All        []networks.Node
	Best       []NodeWithPing
	Discovered []DiscoveredNode
	TimeoutMs  int
	Gas        *networks.GasConfig
}
