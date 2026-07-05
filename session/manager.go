package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/atop0914/llmtrace"
)

// Manager manages multiple conversation sessions.
type Manager struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	maxSessions int
	maxTurns    int
	defaultTTL  time.Duration
}

// ManagerConfig configures the session manager.
type ManagerConfig struct {
	MaxSessions int
	MaxTurns    int
	DefaultTTL  time.Duration
}

// ManagerOption configures the manager.
type ManagerOption func(*ManagerConfig)

// WithMaxSessions sets the maximum number of concurrent sessions.
// Oldest sessions are evicted when the limit is reached.
func WithMaxSessions(n int) ManagerOption {
	return func(c *ManagerConfig) {
		c.MaxSessions = n
	}
}

// WithDefaultTTL sets the default TTL for new sessions.
func WithDefaultTTL(ttl time.Duration) ManagerOption {
	return func(c *ManagerConfig) {
		c.DefaultTTL = ttl
	}
}

// WithManagerMaxTurns sets the default max turns for new sessions.
func WithManagerMaxTurns(n int) ManagerOption {
	return func(c *ManagerConfig) {
		c.MaxTurns = n
	}
}

// NewManager creates a new session manager.
func NewManager(opts ...ManagerOption) *Manager {
	cfg := &ManagerConfig{
		MaxSessions: 1000,
		MaxTurns:    100,
		DefaultTTL:  24 * time.Hour,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Manager{
		sessions:    make(map[string]*Session),
		maxSessions: cfg.MaxSessions,
		maxTurns:    cfg.MaxTurns,
		defaultTTL:  cfg.DefaultTTL,
	}
}

// Create creates a new session with the given options.
func (m *Manager) Create(opts ...SessionOption) *Session {
	cfg := &SessionConfig{
		TTL:      m.defaultTTL,
		MaxTurns: m.maxTurns,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	s := &Session{
		id:       generateID(),
		metadata: cfg.Metadata,
		created:  time.Now(),
		updated:  time.Now(),
		ttl:      cfg.TTL,
		maxTurns: cfg.MaxTurns,
	}

	if cfg.SystemPrompt != "" {
		s.messages = []llmtrace.Message{
			{Role: llmtrace.RoleSystem, Content: cfg.SystemPrompt},
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Evict oldest if at capacity
	if len(m.sessions) >= m.maxSessions {
		m.evictOldest()
	}

	m.sessions[s.id] = s
	return s
}

// Get retrieves a session by ID.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, false
	}
	if s.IsExpired() {
		return nil, false
	}
	return s, true
}

// Delete removes a session by ID.
func (m *Manager) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	return ok
}

// Count returns the number of active sessions.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Cleanup removes expired sessions.
func (m *Manager) Cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for id, s := range m.sessions {
		if s.IsExpired() {
			delete(m.sessions, id)
			removed++
		}
	}
	return removed
}

// List returns all active session IDs.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.sessions))
	for id, s := range m.sessions {
		if !s.IsExpired() {
			ids = append(ids, id)
		}
	}
	return ids
}

// evictOldest removes the oldest session. Must be called with mu held.
func (m *Manager) evictOldest() {
	var oldestID string
	var oldestTime time.Time
	for id, s := range m.sessions {
		if oldestID == "" || s.created.Before(oldestTime) {
			oldestID = id
			oldestTime = s.created
		}
	}
	if oldestID != "" {
		delete(m.sessions, oldestID)
	}
}

// generateID creates a random session ID.
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "sess_" + hex.EncodeToString(b)
}
