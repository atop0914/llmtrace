package session

import (
	"sync"
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

func TestManager_Create(t *testing.T) {
	m := NewManager()
	s := m.Create()
	if s.ID() == "" {
		t.Error("session should have an ID")
	}
	if m.Count() != 1 {
		t.Errorf("expected 1 session, got %d", m.Count())
	}
}

func TestManager_CreateWithOptions(t *testing.T) {
	m := NewManager()
	s := m.Create(
		WithSystemPrompt("You are a coder."),
		WithMetadata("project", "llmtrace"),
		WithMaxTurns(5),
		WithSessionTTL(10*time.Minute),
	)

	msgs := s.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != llmtrace.RoleSystem {
		t.Errorf("expected system role, got %s", msgs[0].Role)
	}
	if msgs[0].Content != "You are a coder." {
		t.Errorf("expected system prompt, got %s", msgs[0].Content)
	}

	if s.Metadata()["project"] != "llmtrace" {
		t.Errorf("expected metadata project=llmtrace")
	}
}

func TestManager_Get(t *testing.T) {
	m := NewManager()
	s := m.Create()

	got, ok := m.Get(s.ID())
	if !ok {
		t.Fatal("expected to find session")
	}
	if got.ID() != s.ID() {
		t.Errorf("expected ID %s, got %s", s.ID(), got.ID())
	}
}

func TestManager_GetNotFound(t *testing.T) {
	m := NewManager()
	_, ok := m.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestManager_GetExpired(t *testing.T) {
	m := NewManager(WithDefaultTTL(50 * time.Millisecond))
	s := m.Create()

	time.Sleep(100 * time.Millisecond)

	_, ok := m.Get(s.ID())
	if ok {
		t.Error("expected expired session to not be found")
	}
}

func TestManager_Delete(t *testing.T) {
	m := NewManager()
	s := m.Create()

	if !m.Delete(s.ID()) {
		t.Error("expected delete to return true")
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 sessions, got %d", m.Count())
	}
}

func TestManager_DeleteNotFound(t *testing.T) {
	m := NewManager()
	if m.Delete("nonexistent") {
		t.Error("expected delete to return false")
	}
}

func TestManager_MaxSessions(t *testing.T) {
	m := NewManager(WithMaxSessions(3))

	s1 := m.Create()
	m.Create()
	m.Create()

	if m.Count() != 3 {
		t.Fatalf("expected 3 sessions, got %d", m.Count())
	}

	// Adding a 4th should evict the oldest
	m.Create()

	if m.Count() != 3 {
		t.Errorf("expected 3 sessions after eviction, got %d", m.Count())
	}

	// s1 should be evicted
	_, ok := m.Get(s1.ID())
	if ok {
		t.Error("expected oldest session to be evicted")
	}
}

func TestManager_Cleanup(t *testing.T) {
	m := NewManager(WithDefaultTTL(50 * time.Millisecond))

	m.Create()
	m.Create()
	m.Create()

	time.Sleep(100 * time.Millisecond)

	removed := m.Cleanup()
	if removed != 3 {
		t.Errorf("expected 3 removed, got %d", removed)
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 sessions, got %d", m.Count())
	}
}

func TestManager_List(t *testing.T) {
	m := NewManager()
	m.Create()
	m.Create()
	m.Create()

	ids := m.List()
	if len(ids) != 3 {
		t.Errorf("expected 3 IDs, got %d", len(ids))
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	m := NewManager(WithMaxSessions(200))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := m.Create()
			m.Get(s.ID())
			m.List()
			m.Count()
		}()
	}
	wg.Wait()

	if m.Count() > 200 {
		t.Errorf("expected at most 200 sessions, got %d", m.Count())
	}
}

func TestManager_DefaultOptions(t *testing.T) {
	m := NewManager()

	if m.maxSessions != 1000 {
		t.Errorf("expected maxSessions 1000, got %d", m.maxSessions)
	}
	if m.maxTurns != 100 {
		t.Errorf("expected maxTurns 100, got %d", m.maxTurns)
	}
	if m.defaultTTL != 24*time.Hour {
		t.Errorf("expected defaultTTL 24h, got %v", m.defaultTTL)
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == id2 {
		t.Error("expected unique IDs")
	}
	if len(id1) != 37 { // "sess_" + 32 hex chars
		t.Errorf("expected ID length 37, got %d", len(id1))
	}
	if id1[:5] != "sess_" {
		t.Errorf("expected prefix sess_, got %s", id1[:5])
	}
}
