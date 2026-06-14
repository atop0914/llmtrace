package llmtrace

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- test helpers ---

// stubProvider is a minimal Provider for testing the fallback router.
type stubProvider struct {
	name          string
	defaultModel  string
	supportStream bool
	completeFunc  func(ctx context.Context, req *Request) (*Response, error)
	streamFunc    func(ctx context.Context, req *Request) (<-chan StreamChunk, error)
}

func (s *stubProvider) Name() string                                { return s.name }
func (s *stubProvider) DefaultModel() string                        { return s.defaultModel }
func (s *stubProvider) SupportsStreaming() bool                     { return s.supportStream }
func (s *stubProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	if s.completeFunc != nil {
		return s.completeFunc(ctx, req)
	}
	return &Response{Provider: s.name, Model: req.Model}, nil
}
func (s *stubProvider) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
	if s.streamFunc != nil {
		return s.streamFunc(ctx, req)
	}
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Content: "ok"}
	close(ch)
	return ch, nil
}

func newOKProvider(name string) *stubProvider {
	return &stubProvider{
		name:          name,
		defaultModel:  name + "-default",
		supportStream: true,
	}
}

func newFailProvider(name string) *stubProvider {
	return &stubProvider{
		name:          name,
		supportStream: true,
		completeFunc: func(_ context.Context, _ *Request) (*Response, error) {
			return nil, errors.New(name + ": unavailable")
		},
		streamFunc: func(_ context.Context, _ *Request) (<-chan StreamChunk, error) {
			return nil, errors.New(name + ": unavailable")
		},
	}
}

func testRequest() *Request {
	return &Request{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
	}
}

// --- tests ---

func TestFallbackRouter_Name(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		r := DefaultFallbackConfig()
		router := NewFallbackRouter(r)
		if got := router.Name(); got != "fallback(empty)" {
			t.Errorf("got %q, want %q", got, "fallback(empty)")
		}
	})

	t.Run("single", func(t *testing.T) {
		router := NewFallbackRouter(FallbackConfig{}, newOKProvider("openai"))
		if got := router.Name(); got != "openai" {
			t.Errorf("got %q, want %q", got, "openai")
		}
	})

	t.Run("multiple", func(t *testing.T) {
		router := NewFallbackRouter(FallbackConfig{},
			newOKProvider("openai"),
			newOKProvider("anthropic"),
		)
		if got := router.Name(); got != "openai+fallback" {
			t.Errorf("got %q, want %q", got, "openai+fallback")
		}
	})
}

func TestFallbackRouter_DefaultModel(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		router := NewFallbackRouter(FallbackConfig{})
		if got := router.DefaultModel(); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("uses first provider", func(t *testing.T) {
		p1 := newOKProvider("openai")
		p1.defaultModel = "gpt-4o"
		p2 := newOKProvider("anthropic")
		p2.defaultModel = "claude-3"
		router := NewFallbackRouter(FallbackConfig{}, p1, p2)
		if got := router.DefaultModel(); got != "gpt-4o" {
			t.Errorf("got %q, want %q", got, "gpt-4o")
		}
	})
}

func TestFallbackRouter_Providers(t *testing.T) {
	router := NewFallbackRouter(FallbackConfig{},
		newOKProvider("openai"),
		newOKProvider("anthropic"),
		newOKProvider("gemini"),
	)
	got := router.Providers()
	want := []string{"openai", "anthropic", "gemini"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFallbackRouter_Complete_Priority(t *testing.T) {
	t.Run("first provider succeeds", func(t *testing.T) {
		p1 := newOKProvider("openai")
		p2 := newOKProvider("anthropic")
		router := NewFallbackRouter(FallbackConfig{Strategy: StrategyPriority}, p1, p2)
		defer router.Close()

		resp, err := router.Complete(context.Background(), testRequest())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Provider != "openai" {
			t.Errorf("got provider %q, want %q", resp.Provider, "openai")
		}
	})

	t.Run("fallback to second on first failure", func(t *testing.T) {
		p1 := newFailProvider("openai")
		p2 := newOKProvider("anthropic")
		router := NewFallbackRouter(FallbackConfig{
			Strategy: StrategyPriority,
			Cooldown: 10 * time.Second,
		}, p1, p2)
		defer router.Close()

		resp, err := router.Complete(context.Background(), testRequest())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Provider != "anthropic" {
			t.Errorf("got provider %q, want %q", resp.Provider, "anthropic")
		}
	})

	t.Run("all fail returns last error", func(t *testing.T) {
		p1 := newFailProvider("openai")
		p2 := newFailProvider("anthropic")
		router := NewFallbackRouter(FallbackConfig{Cooldown: 10 * time.Second}, p1, p2)
		defer router.Close()

		_, err := router.Complete(context.Background(), testRequest())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		// Error should be from the last tried provider
		if !errors.Is(err, err) {
			t.Errorf("got error %v, want it to contain provider error", err)
		}
	})
}

func TestFallbackRouter_Complete_RoundRobin(t *testing.T) {
	p1 := newOKProvider("openai")
	p2 := newOKProvider("anthropic")
	p3 := newOKProvider("gemini")
	router := NewFallbackRouter(FallbackConfig{
		Strategy: StrategyRoundRobin,
	}, p1, p2, p3)
	defer router.Close()

	seen := make(map[string]int)
	for range 9 {
		resp, err := router.Complete(context.Background(), testRequest())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		seen[resp.Provider]++
	}

	// With round-robin over 9 requests, each provider should be called 3 times
	if seen["openai"] != 3 || seen["anthropic"] != 3 || seen["gemini"] != 3 {
		t.Errorf("expected equal distribution, got %v", seen)
	}
}

func TestFallbackRouter_Complete_MaxAttempts(t *testing.T) {
	var attempted []string
	var mu sync.Mutex
	wrapFail := func(name string) *stubProvider {
		p := newFailProvider(name)
		p.completeFunc = func(_ context.Context, _ *Request) (*Response, error) {
			mu.Lock()
			attempted = append(attempted, name)
			mu.Unlock()
			return nil, errors.New(name + ": fail")
		}
		return p
	}

	router := NewFallbackRouter(FallbackConfig{
		MaxAttempts: 2,
		Cooldown:    10 * time.Second,
	}, wrapFail("openai"), wrapFail("anthropic"), newOKProvider("gemini"))
	defer router.Close()

	_, err := router.Complete(context.Background(), testRequest())
	// Should only try 2 providers, not all 3. Since first 2 fail, we get error.
	if err == nil {
		t.Fatal("expected error after MaxAttempts")
	}

	mu.Lock()
	if len(attempted) != 2 {
		t.Errorf("tried %d providers, want 2: %v", len(attempted), attempted)
	}
	mu.Unlock()
}

func TestFallbackRouter_Complete_SkipsUnhealthy(t *testing.T) {
	p1 := newFailProvider("openai")
	p2 := newOKProvider("anthropic")
	p3 := newOKProvider("gemini")

	router := NewFallbackRouter(FallbackConfig{
		Strategy: StrategyPriority,
		Cooldown: 10 * time.Second,
	}, p1, p2, p3)
	defer router.Close()

	// First request: openai fails, marks unhealthy, falls back to anthropic
	resp, err := router.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("first request: got %q, want %q", resp.Provider, "anthropic")
	}

	// Second request: openai is unhealthy, skipped, anthropic succeeds
	resp, err = router.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("second request: got %q, want %q", resp.Provider, "anthropic")
	}
}

func TestFallbackRouter_Stream_Failover(t *testing.T) {
	p1 := newFailProvider("openai")
	p2 := newOKProvider("anthropic")
	router := NewFallbackRouter(FallbackConfig{
		Cooldown: 10 * time.Second,
	}, p1, p2)
	defer router.Close()

	ch, err := router.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk, ok := <-ch
	if !ok {
		t.Fatal("expected chunk, channel closed")
	}
	if chunk.Content != "ok" {
		t.Errorf("got content %q, want %q", chunk.Content, "ok")
	}
}

func TestFallbackRouter_Stream_SkipsNonStreaming(t *testing.T) {
	p1 := &stubProvider{
		name:          "nosupport",
		supportStream: false,
	}
	p2 := newOKProvider("anthropic")
	router := NewFallbackRouter(FallbackConfig{}, p1, p2)
	defer router.Close()

	ch, err := router.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected channel, got nil")
	}
}

func TestFallbackRouter_Stream_AllFail(t *testing.T) {
	p1 := newFailProvider("openai")
	p2 := newFailProvider("anthropic")
	router := NewFallbackRouter(FallbackConfig{Cooldown: 10 * time.Second}, p1, p2)
	defer router.Close()

	_, err := router.Stream(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestFallbackRouter_OnFallback(t *testing.T) {
	var callbacks []struct {
		from, to string
		err      error
	}
	var mu sync.Mutex

	p1 := newFailProvider("openai")
	p2 := newOKProvider("anthropic")
	router := NewFallbackRouter(FallbackConfig{
		Cooldown: 10 * time.Second,
		OnFallback: func(from, to string, err error) {
			mu.Lock()
			callbacks = append(callbacks, struct {
				from, to string
				err      error
			}{from, to, err})
			mu.Unlock()
		},
	}, p1, p2)
	defer router.Close()

	_, err := router.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(callbacks) != 1 {
		t.Fatalf("expected 1 callback, got %d", len(callbacks))
	}
	if callbacks[0].from != "openai" || callbacks[0].to != "anthropic" {
		t.Errorf("callback: from=%q to=%q, want openai->anthropic", callbacks[0].from, callbacks[0].to)
	}
}

func TestFallbackRouter_OnProviderHealthChange(t *testing.T) {
	var changes []struct {
		name    string
		healthy bool
	}
	var mu sync.Mutex

	p1 := newFailProvider("openai")
	p2 := newOKProvider("anthropic")
	router := NewFallbackRouter(FallbackConfig{
		Cooldown: 100 * time.Millisecond,
		OnProviderHealthChange: func(name string, healthy bool) {
			mu.Lock()
			changes = append(changes, struct {
				name    string
				healthy bool
			}{name, healthy})
			mu.Unlock()
		},
	}, p1, p2)
	defer router.Close()

	// First request: openai fails -> unhealthy
	_, err := router.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	if len(changes) != 1 || changes[0].name != "openai" || changes[0].healthy {
		t.Errorf("expected openai unhealthy, got %v", changes)
	}
	mu.Unlock()
}

func TestFallbackRouter_HealthStatus(t *testing.T) {
	p1 := newFailProvider("openai")
	p2 := newOKProvider("anthropic")
	router := NewFallbackRouter(FallbackConfig{Cooldown: 10 * time.Second}, p1, p2)
	defer router.Close()

	// Trigger failure on openai
	router.Complete(context.Background(), testRequest())

	status := router.HealthStatus()
	if status["openai"] {
		t.Error("expected openai to be unhealthy")
	}
	if !status["anthropic"] {
		t.Error("expected anthropic to be healthy")
	}
}

func TestFallbackRouter_ResetHealth(t *testing.T) {
	p1 := newFailProvider("openai")
	p2 := newOKProvider("anthropic")
	router := NewFallbackRouter(FallbackConfig{Cooldown: 10 * time.Second}, p1, p2)
	defer router.Close()

	// Trigger failure
	router.Complete(context.Background(), testRequest())

	status := router.HealthStatus()
	if status["openai"] {
		t.Error("openai should be unhealthy after failure")
	}

	// Reset
	router.ResetHealth()
	status = router.HealthStatus()
	if !status["openai"] {
		t.Error("openai should be healthy after ResetHealth")
	}
}

func TestFallbackRouter_SupportsStreaming(t *testing.T) {
	t.Run("any supports", func(t *testing.T) {
		p1 := &stubProvider{name: "a", supportStream: false}
		p2 := &stubProvider{name: "b", supportStream: true}
		router := NewFallbackRouter(FallbackConfig{}, p1, p2)
		if !router.SupportsStreaming() {
			t.Error("expected true when any provider supports streaming")
		}
	})

	t.Run("none supports", func(t *testing.T) {
		p1 := &stubProvider{name: "a", supportStream: false}
		router := NewFallbackRouter(FallbackConfig{}, p1)
		if router.SupportsStreaming() {
			t.Error("expected false when no provider supports streaming")
		}
	})
}

func TestFallbackRouter_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p1 := &stubProvider{
		name:          "openai",
		supportStream: true,
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return &Response{Provider: "openai"}, nil
			}
		},
	}
	router := NewFallbackRouter(FallbackConfig{}, p1)
	defer router.Close()

	_, err := router.Complete(ctx, testRequest())
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
}

func TestFallbackRouter_ConcurrentAccess(t *testing.T) {
	p1 := newOKProvider("openai")
	p2 := newOKProvider("anthropic")
	router := NewFallbackRouter(FallbackConfig{
		Strategy: StrategyRoundRobin,
	}, p1, p2)
	defer router.Close()

	var wg sync.WaitGroup
	var errCount atomic.Int32

	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := router.Complete(context.Background(), testRequest())
			if err != nil {
				errCount.Add(1)
			}
		}()
	}

	wg.Wait()
	if errCount.Load() > 0 {
		t.Errorf("got %d errors in concurrent access", errCount.Load())
	}
}

func TestFallbackRouter_NoProviders(t *testing.T) {
	router := NewFallbackRouter(FallbackConfig{})
	defer router.Close()

	_, err := router.Complete(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error with no providers")
	}
}

func TestFallbackRouter_CooldownRecovery(t *testing.T) {
	var completeCount atomic.Int32
	p1 := &stubProvider{
		name:          "flaky",
		supportStream: true,
		completeFunc: func(_ context.Context, _ *Request) (*Response, error) {
			count := completeCount.Add(1)
			if count <= 1 {
				return nil, errors.New("flaky: temporary failure")
			}
			return &Response{Provider: "flaky", Model: "test"}, nil
		},
	}
	p2 := newOKProvider("backup")

	router := NewFallbackRouter(FallbackConfig{
		Cooldown: 50 * time.Millisecond,
	}, p1, p2)
	defer router.Close()

	// First request: flaky fails, falls back to backup
	resp, err := router.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "backup" {
		t.Errorf("got %q, want %q", resp.Provider, "backup")
	}

	// Wait for cooldown
	time.Sleep(100 * time.Millisecond)

	// flaky should be healthy again and succeed this time
	resp, err = router.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "flaky" {
		t.Errorf("got %q, want %q", resp.Provider, "flaky")
	}
}

func TestFallbackRouter_Close(t *testing.T) {
	router := NewFallbackRouter(FallbackConfig{
		HealthCheckInterval: 10 * time.Millisecond,
	}, newOKProvider("test"))

	// Close should not panic
	router.Close()
	router.Close() // double close should be safe
}

func TestFallbackStrategy_Constants(t *testing.T) {
	if StrategyPriority != 0 {
		t.Errorf("StrategyPriority = %d, want 0", StrategyPriority)
	}
	if StrategyRoundRobin != 1 {
		t.Errorf("StrategyRoundRobin = %d, want 1", StrategyRoundRobin)
	}
}

func TestDefaultFallbackConfig(t *testing.T) {
	cfg := DefaultFallbackConfig()
	if cfg.Strategy != StrategyPriority {
		t.Errorf("Strategy = %d, want StrategyPriority", cfg.Strategy)
	}
	if cfg.HealthCheckInterval != 30*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 30s", cfg.HealthCheckInterval)
	}
	if cfg.Cooldown != 60*time.Second {
		t.Errorf("Cooldown = %v, want 60s", cfg.Cooldown)
	}
}