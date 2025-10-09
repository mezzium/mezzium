package peers

func NewStore() *Store {
	return &Store{
		peers: make(map[string]Peer),
	}
}

func (s *Store) Add(p Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[p.ID] = p
}

func (s *Store) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.peers, id)
}

func (s *Store) List() []Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}
	return out
}
func (s *Store) OnFailure(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.peers[id]; ok {
		p.Failures++
		if p.Failures >= 2 {
			delete(s.peers, id)
		} else {
			s.peers[id] = p
		}
	}
}
func (s *Store) OnSuccess(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.peers[id]; ok {
		p.Failures = 0
		s.peers[id] = p
	}
}

func (s *Store) Exists(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.peers[id]
	return ok
}
