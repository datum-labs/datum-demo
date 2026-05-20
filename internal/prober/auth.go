package prober

import "sync"

// BasicAuthState holds the credentials the prober should attach to the Datum
// probe's Authorization header. It mirrors what we wrote into the
// SecurityPolicy's Secret; we keep the plaintext only here, in memory, so it
// is gone on restart (which is fine — re-enabling the toggle repopulates it).
type BasicAuthState struct {
	mu       sync.RWMutex
	enabled  bool
	username string
	password string
}

func NewBasicAuthState() *BasicAuthState { return &BasicAuthState{} }

func (s *BasicAuthState) Snapshot() (enabled bool, username, password string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled, s.username, s.password
}

func (s *BasicAuthState) Set(username, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = true
	s.username = username
	s.password = password
}

func (s *BasicAuthState) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = false
	s.username = ""
	s.password = ""
}
