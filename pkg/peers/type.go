package peers

import "sync"

type Peer struct {
	ID       string `json:"id"`
	Addr     string `json:"addr"`
	Failures int
}

type Store struct {
	mu    sync.RWMutex
	peers map[string]Peer
}
