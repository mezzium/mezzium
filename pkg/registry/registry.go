package registry

import (
	"sort"
	"strings"
	"time"

	"github.com/shuliakovsky/mezzium/pkg/networks"
)

func New() *Registry { return &Registry{State: map[string]*NetworkState{}} }

func (r *Registry) InitFromConfigs(cfgs map[string]networks.NetworkConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, c := range cfgs {
		copyNodes := make([]networks.Node, len(c.Nodes))
		copy(copyNodes, c.Nodes)
		r.State[name] = &NetworkState{
			Protocol:  c.Protocol,
			Route:     c.Route,
			TimeoutMs: c.TimeoutMs,
			All:       copyNodes,
			Best:      nil,
			Gas:       c.Gas,
		}
	}
}

func (r *Registry) SetBest(name string, best []NodeWithPing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.State[name]; ok {
		s.Best = best
	}
}

func (r *Registry) Best(name string) []NodeWithPing {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.State[name]; ok && len(s.Best) > 0 {
		out := make([]NodeWithPing, len(s.Best))
		copy(out, s.Best)
		return out
	}
	return nil
}

func (r *Registry) All() map[string]*NetworkState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]*NetworkState, len(r.State))
	for k, v := range r.State {
		out[k] = v
	}
	return out
}

func PickFastestPerPriority(nodes []NodeWithPing) []NodeWithPing {
	m := map[int][]NodeWithPing{}
	for _, n := range nodes {
		if !n.Alive {
			continue
		}
		m[n.Priority] = append(m[n.Priority], n)
	}
	var best []NodeWithPing
	for _, group := range m {
		fastest := group[0]
		for _, cur := range group[1:] {
			if cur.Ping < fastest.Ping {
				fastest = cur
			}
		}
		best = append(best, fastest)
	}
	sort.Slice(best, func(i, j int) bool { return best[i].Priority < best[j].Priority })
	return best
}
func (r *Registry) AddNetwork(cfg networks.NetworkConfig, best []NodeWithPing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.TrimPrefix(cfg.Route, "/")
	r.State[key] = &NetworkState{
		Protocol:  cfg.Protocol,
		Route:     cfg.Route,
		TimeoutMs: cfg.TimeoutMs,
		All:       cfg.Nodes,
		Best:      best,
	}
}

func (r *Registry) ProtocolOf(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.State[name]; ok {
		return s.Protocol
	}
	return ""
}

func (r *Registry) AllBestOrEmpty() map[string][]NodeWithPing {
	res := make(map[string][]NodeWithPing)
	for name, st := range r.All() {
		if len(st.Best) == 0 {
			res[name] = []NodeWithPing{}
			continue
		}
		res[name] = st.Best
	}
	return res
}

func (r *Registry) AddNode(net string, n networks.Node) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.State[net]; ok {
		s.All = append(s.All, n)
	}
}

func (r *Registry) AppendBest(net string, n NodeWithPing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.State[net]; ok {
		s.Best = append(s.Best, n)
	}
}
func (r *Registry) PruneAndMerge(ttl time.Duration) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.State {
		//  Cleaning up outdated nodes
		var fresh []DiscoveredNode
		for _, dn := range st.Discovered {
			if dn.ExpiresAt.After(now) {
				fresh = append(fresh, dn)
			}
		}
		st.Discovered = fresh

		// Merge with All
		for _, dn := range st.Discovered {
			// no duplicates
			dup := false
			for _, n := range st.All {
				if n.URL == dn.Node.URL {
					dup = true
					break
				}
			}
			if !dup {
				st.All = append(st.All, dn.Node)
			}
		}
	}
}
func (r *Registry) Exists(route string) bool {
	route = strings.ToLower(strings.Trim(route, "/"))
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.State[route]
	return ok
}

func (r *Registry) TimeoutMs(network string) int {
	if s, ok := r.State[network]; ok {
		return s.TimeoutMs
	}
	return 0
}

// RemoveNodeEverywhere removes a node URL from All/Best/Discovered across all networks.
func (r *Registry) RemoveNodeEverywhere(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.State {
		// All
		if len(st.All) > 0 {
			dst := st.All[:0]
			for _, n := range st.All {
				if n.URL != url {
					dst = append(dst, n)
				}
			}
			st.All = dst
		}
		// Best
		if len(st.Best) > 0 {
			dst := st.Best[:0]
			for _, n := range st.Best {
				if n.URL != url {
					dst = append(dst, n)
				}
			}
			st.Best = dst
		}
		// Discovered
		if len(st.Discovered) > 0 {
			dst := st.Discovered[:0]
			for _, dn := range st.Discovered {
				if dn.Node.URL != url {
					dst = append(dst, dn)
				}
			}
			st.Discovered = dst
		}
	}
}
func (r *Registry) GasConfigOf(name string) *networks.GasConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.State[name]; ok {
		return s.Gas
	}
	return nil
}
