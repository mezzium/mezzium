package gossip

import "github.com/shuliakovsky/mezzium/pkg/peers"

type GossipMessage struct {
	From  string       `json:"from"`
	Peers []peers.Peer `json:"peers"`
}
type NodeAdvert struct {
	URL      string `json:"url"`
	Priority int    `json:"priority"`
	Protocol string `json:"protocol"`
	Alive    bool   `json:"alive"`
	Ping     int64  `json:"ping"`
}

type NetworkAdvert struct {
	Name     string       `json:"name"`
	Protocol string       `json:"protocol"`
	Nodes    []NodeAdvert `json:"nodes"`
	Ts       int64        `json:"ts"`
}

type StateMessage struct {
	From     string          `json:"from"`
	Networks []NetworkAdvert `json:"networks"`
}
