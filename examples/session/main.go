// Example: Multi-turn conversation session tracking
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/session"
)

func main() {
	// Create a session manager with custom options
	mgr := session.NewManager(
		session.WithMaxSessions(100),
		session.WithDefaultTTL(1 * time.Hour),
		session.WithManagerMaxTurns(20),
	)

	// Create a session with system prompt
	sess := mgr.Create(
		session.WithSystemPrompt("You are a helpful Go programming assistant."),
		session.WithMetadata("user_id", "user-123"),
	)

	fmt.Printf("Created session: %s\n", sess.ID())

	// Simulate a multi-turn conversation
	questions := []struct {
		user      string
		assistant string
	}{
		{"What is Go?", "Go is an open-source programming language designed at Google."},
		{"What are its main features?", "Go features include goroutines, channels, and a simple type system."},
		{"How do I create a goroutine?", "Use the 'go' keyword followed by a function call: go myFunction()."},
	}

	for _, q := range questions {
		sess.AddUserMessage(q.user)
		fmt.Printf("\n[User] %s\n", q.user)
		fmt.Printf("Turn count: %d\n", sess.TurnCount())

		// In a real app, you would call the LLM here:
		// resp, err := tracer.Chat(ctx, sess.Request("gpt-4o"), provider)
		sess.AddAssistantMessage(q.assistant)
		fmt.Printf("[Assistant] %s\n", q.assistant)
	}

	// Check token estimate
	fmt.Printf("\nEstimated tokens: %d\n", sess.EstimateTokens())

	// Get the full conversation history
	msgs := sess.Messages()
	fmt.Printf("Total messages: %d\n", len(msgs))

	// List all sessions
	fmt.Printf("\nActive sessions: %d\n", mgr.Count())
	for _, id := range mgr.List() {
		fmt.Printf("  - %s\n", id)
	}

	// Demonstrate session retrieval
	retrieved, ok := mgr.Get(sess.ID())
	if ok {
		fmt.Printf("\nRetrieved session %s with %d messages\n",
			retrieved.ID(), len(retrieved.Messages()))
	}

	// Demonstrate session clearing
	sess.Clear()
	fmt.Printf("After clear: %d messages\n", len(sess.Messages()))

	// Demonstrate TTL expiration
	shortMgr := session.NewManager(session.WithDefaultTTL(100 * time.Millisecond))
	shortSess := shortMgr.Create()
	fmt.Printf("\nShort-lived session: %s\n", shortSess.ID())

	time.Sleep(150 * time.Millisecond)

	_, expired := shortMgr.Get(shortSess.ID())
	fmt.Printf("Session expired: %v\n", !expired)

	// Suppress unused import warnings
	_ = context.Background()
	_ = llmtrace.NewTracer("example")
	log.Println("Session example completed")
}
