package tokencount

import (
	"fmt"
	"strings"
)

// Manager provides context window validation and cost estimation.
type Manager struct {
	registry *ModelRegistry
}

// NewManager creates a Manager with the default model registry.
func NewManager() *Manager {
	return &Manager{registry: DefaultModels()}
}

// NewManagerWithRegistry creates a Manager with a custom model registry.
func NewManagerWithRegistry(r *ModelRegistry) *Manager {
	return &Manager{registry: r}
}

// Registry returns the underlying model registry for modifications.
func (m *Manager) Registry() *ModelRegistry {
	return m.registry
}

// CheckResult holds the result of a context window validation.
type CheckResult struct {
	// FitsContext is true if the request fits within the model's context window.
	FitsContext bool

	// InputTokens is the estimated input token count.
	InputTokens int

	// AvailableForOutput is the remaining tokens available for output.
	AvailableForOutput int

	// RequestedMaxTokens is the max_tokens value from the request (0 if unset).
	RequestedMaxTokens int

	// SuggestedMaxTokens is the recommended max_tokens to avoid overflow.
	// Equals min(AvailableForOutput, Model.MaxOutputTokens).
	SuggestedMaxTokens int

	// UsageRatio is inputTokens / contextWindow (0.0-1.0+). Values > 1.0 mean overflow.
	UsageRatio float64

	// Model is the resolved model info.
	Model ModelInfo

	// Error describes any issue (unknown model, overflow, etc.). Empty if OK.
	Error string
}

// ValidateRequest checks if a request with the given messages fits within
// the model's context window, and suggests a safe max_tokens value.
//
// Messages are joined with role prefixes for token estimation.
// maxTokens is the caller's requested output limit (0 = use model default).
func (m *Manager) ValidateRequest(model string, messages []Message, maxTokens int) CheckResult {
	info, ok := m.registry.Get(model)
	if !ok {
		return CheckResult{Error: fmt.Sprintf("unknown model: %s", model)}
	}

	// Estimate input tokens
	inputText := formatMessages(messages)
	inputTokens := EstimateTokens(inputText, info.CharsPerToken)

	return m.validateFromTokens(model, info, inputTokens, maxTokens)
}

// ValidateText is a simpler variant that takes raw text instead of messages.
func (m *Manager) ValidateText(model string, inputText string, maxTokens int) CheckResult {
	info, ok := m.registry.Get(model)
	if !ok {
		return CheckResult{Error: fmt.Sprintf("unknown model: %s", model)}
	}

	inputTokens := EstimateTokens(inputText, info.CharsPerToken)
	return m.validateFromTokens(model, info, inputTokens, maxTokens)
}

// ValidateTokenCount is the simplest variant — pass pre-counted tokens.
func (m *Manager) ValidateTokenCount(model string, inputTokens, maxTokens int) CheckResult {
	info, ok := m.registry.Get(model)
	if !ok {
		return CheckResult{Error: fmt.Sprintf("unknown model: %s", model)}
	}
	return m.validateFromTokens(model, info, inputTokens, maxTokens)
}

func (m *Manager) validateFromTokens(model string, info ModelInfo, inputTokens, maxTokens int) CheckResult {
	available := info.ContextWindow - inputTokens

	suggested := min(available, info.MaxOutputTokens)
	if suggested < 0 {
		suggested = 0
	}

	fits := available > 0 && (maxTokens <= 0 || maxTokens <= available)

	ratio := float64(inputTokens) / float64(info.ContextWindow)

	errMsg := ""
	if !fits {
		errMsg = fmt.Sprintf("context overflow: input %d tokens, context window %d, available %d",
			inputTokens, info.ContextWindow, available)
	}

	return CheckResult{
		FitsContext:        fits,
		InputTokens:        inputTokens,
		AvailableForOutput: available,
		RequestedMaxTokens: maxTokens,
		SuggestedMaxTokens: suggested,
		UsageRatio:         ratio,
		Model:              info,
		Error:              errMsg,
	}
}

// EstimateCost calculates the estimated cost in USD for a request.
func (m *Manager) EstimateCost(model string, inputTokens, outputTokens int) (float64, error) {
	info, ok := m.registry.Get(model)
	if !ok {
		return 0, fmt.Errorf("unknown model: %s", model)
	}
	inputCost := float64(inputTokens) / 1000.0 * info.InputCostPer1K
	outputCost := float64(outputTokens) / 1000.0 * info.OutputCostPer1K
	return inputCost + outputCost, nil
}

// EstimateMessagesCost estimates the cost of a conversation given messages
// and an expected output token count.
func (m *Manager) EstimateMessagesCost(model string, messages []Message, expectedOutputTokens int) (float64, error) {
	info, ok := m.registry.Get(model)
	if !ok {
		return 0, fmt.Errorf("unknown model: %s", model)
	}
	inputText := formatMessages(messages)
	inputTokens := EstimateTokens(inputText, info.CharsPerToken)
	return m.EstimateCost(model, inputTokens, expectedOutputTokens)
}

// RecommendModel finds the cheapest model that fits the given input.
// Returns empty string if no model fits.
func (m *Manager) RecommendModel(messages []Message, maxTokens int) string {
	inputText := formatMessages(messages)

	var bestModel string
	var bestCost float64 = -1

	for _, name := range m.registry.List() {
		info, ok := m.registry.Get(name)
		if !ok {
			continue
		}
		inputTokens := EstimateTokens(inputText, info.CharsPerToken)
		if inputTokens+maxTokens > info.ContextWindow {
			continue
		}
		cost := float64(inputTokens)/1000.0*info.InputCostPer1K +
			float64(maxTokens)/1000.0*info.OutputCostPer1K
		if bestCost < 0 || cost < bestCost {
			bestCost = cost
			bestModel = name
		}
	}
	return bestModel
}

// TruncateToFit truncates messages from the beginning to fit within the
// model's context window, preserving the system message if present.
// Returns the truncated messages and whether truncation was needed.
func (m *Manager) TruncateToFit(model string, messages []Message, maxTokens int) ([]Message, bool) {
	info, ok := m.registry.Get(model)
	if !ok {
		return messages, false
	}

	inputText := formatMessages(messages)
	inputTokens := EstimateTokens(inputText, info.CharsPerToken)
	available := info.ContextWindow - maxTokens

	if available <= 0 {
		return nil, true
	}

	if inputTokens <= available {
		return messages, false
	}

	// Preserve system message if present
	var systemMsg *Message
	start := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		msg := messages[0]
		systemMsg = &msg
		start = 1
		// Recalculate available after accounting for system message
		sysTokens := EstimateTokens(formatMessages([]Message{msg}), info.CharsPerToken)
		available -= sysTokens
		if available <= 0 {
			return []Message{*systemMsg}, true
		}
	}

	// Drop messages from the start (after system) until we fit
	candidates := messages[start:]
	for i := 0; i < len(candidates); i++ {
		remaining := candidates[i:]
		text := formatMessages(remaining)
		tokens := EstimateTokens(text, info.CharsPerToken)
		if tokens <= available {
			result := remaining
			if systemMsg != nil {
				result = append([]Message{*systemMsg}, result...)
			}
			return result, true
		}
	}

	// Nothing fits
	if systemMsg != nil {
		return []Message{*systemMsg}, true
	}
	return nil, true
}

// Message is a conversation message for token counting purposes.
// This is a local copy to avoid circular imports with the root package.
type Message struct {
	Role    string
	Content string
}

// formatMessages formats messages into a single string for token estimation.
func formatMessages(messages []Message) string {
	var b strings.Builder
	for _, msg := range messages {
		fmt.Fprintf(&b, "<|%s|>\n%s\n", msg.Role, msg.Content)
	}
	return b.String()
}
