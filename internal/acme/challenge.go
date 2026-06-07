package acme

import "sync"

// ChallengeStore provides thread-safe storage for ACME DNS-01 challenge tokens.
type ChallengeStore interface {
	Set(fqdn, value string)
	Delete(fqdn, value string)
	GetTXT(fqdn string) []string
}

type store struct {
	mu sync.RWMutex
	m  map[string][]string
}

var _ ChallengeStore = (*store)(nil)

func NewChallengeStore() ChallengeStore {
	return &store{m: make(map[string][]string)}
}

func (c *store) Set(fqdn, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.m[fqdn] = append(c.m[fqdn], value)
}

func (c *store) Delete(fqdn, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	vals := c.m[fqdn]
	for i, v := range vals {
		if v == value {
			c.m[fqdn] = append(vals[:i], vals[i+1:]...)
			break
		}
	}

	if len(c.m[fqdn]) == 0 {
		delete(c.m, fqdn)
	}
}

func (c *store) GetTXT(fqdn string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return append([]string(nil), c.m[fqdn]...)
}
