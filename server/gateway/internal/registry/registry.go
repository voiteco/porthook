// SPDX-License-Identifier: AGPL-3.0-only

package registry

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrTunnelIDAlreadyRegistered  = errors.New("tunnel id already registered")
	ErrSubdomainAlreadyRegistered = errors.New("subdomain already registered")
)

type Session struct {
	TunnelID        string
	Subdomain       string
	PublicURL       string
	LocalTarget     string
	Protocol        string
	AgentVersion    string
	ProtocolVersion string
	Capabilities    []string
	CreatedAt       time.Time
}

// HasCapability reports whether the connected agent declared capability
// during protocol negotiation.
func (s *Session) HasCapability(capability string) bool {
	if s == nil {
		return false
	}
	for _, c := range s.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

type Registry struct {
	mu          sync.RWMutex
	byTunnelID  map[string]*Session
	bySubdomain map[string]*Session
}

func New() *Registry {
	return &Registry{
		byTunnelID:  make(map[string]*Session),
		bySubdomain: make(map[string]*Session),
	}
}

func (r *Registry) Register(session *Session) error {
	if session == nil {
		return errors.New("session is required")
	}
	if session.TunnelID == "" {
		return errors.New("tunnel id is required")
	}
	if session.Subdomain == "" {
		return errors.New("subdomain is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byTunnelID[session.TunnelID]; exists {
		return ErrTunnelIDAlreadyRegistered
	}
	if _, exists := r.bySubdomain[session.Subdomain]; exists {
		return ErrSubdomainAlreadyRegistered
	}

	r.byTunnelID[session.TunnelID] = session
	r.bySubdomain[session.Subdomain] = session
	return nil
}

func (r *Registry) LookupByTunnelID(tunnelID string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.byTunnelID[tunnelID]
	return session, ok
}

func (r *Registry) LookupBySubdomain(subdomain string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.bySubdomain[subdomain]
	return session, ok
}

func (r *Registry) Unregister(tunnelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.byTunnelID[tunnelID]
	if !ok {
		return
	}

	delete(r.byTunnelID, tunnelID)
	delete(r.bySubdomain, session.Subdomain)
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byTunnelID)
}

func (r *Registry) List() []Session {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sessions := make([]Session, 0, len(r.byTunnelID))
	for _, session := range r.byTunnelID {
		sessions = append(sessions, *session)
	}
	return sessions
}
