package llmtrace

import "testing"

func TestFloat64Ptr(t *testing.T) {
	p := Float64Ptr(3.14)
	if p == nil {
		t.Fatal("Float64Ptr returned nil")
	}
	if *p != 3.14 {
		t.Errorf("*p = %f, want 3.14", *p)
	}
}

func TestFloat64Ptr_Zero(t *testing.T) {
	p := Float64Ptr(0)
	if p == nil {
		t.Fatal("Float64Ptr(0) returned nil")
	}
	if *p != 0 {
		t.Errorf("*p = %f, want 0", *p)
	}
}

func TestIntPtr(t *testing.T) {
	p := IntPtr(42)
	if p == nil {
		t.Fatal("IntPtr returned nil")
	}
	if *p != 42 {
		t.Errorf("*p = %d, want 42", *p)
	}
}

func TestIntPtr_Zero(t *testing.T) {
	p := IntPtr(0)
	if p == nil {
		t.Fatal("IntPtr(0) returned nil")
	}
	if *p != 0 {
		t.Errorf("*p = %d, want 0", *p)
	}
}

func TestRoles(t *testing.T) {
	roles := []struct {
		role Role
		want string
	}{
		{RoleSystem, "system"},
		{RoleUser, "user"},
		{RoleAssistant, "assistant"},
		{RoleTool, "tool"},
	}
	for _, tt := range roles {
		if string(tt.role) != tt.want {
			t.Errorf("Role %v = %q, want %q", tt.role, string(tt.role), tt.want)
		}
	}
}

func TestMessage_Struct(t *testing.T) {
	msg := Message{
		Role:    RoleUser,
		Content: "Hello, world!",
	}
	if msg.Role != RoleUser {
		t.Errorf("Role = %v, want %v", msg.Role, RoleUser)
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", msg.Content, "Hello, world!")
	}
}

func TestRequest_Struct(t *testing.T) {
	req := &Request{
		Model:       "gpt-4o",
		Temperature: Float64Ptr(0.7),
		TopP:        Float64Ptr(0.9),
		MaxTokens:   IntPtr(100),
		Stop:        []string{"END"},
		Messages: []Message{
			{Role: RoleSystem, Content: "You are helpful."},
			{Role: RoleUser, Content: "Hello"},
		},
		Extra: map[string]any{"key": "value"},
	}

	if req.Model != "gpt-4o" {
		t.Errorf("Model = %q", req.Model)
	}
	if *req.Temperature != 0.7 {
		t.Errorf("Temperature = %f", *req.Temperature)
	}
	if len(req.Messages) != 2 {
		t.Errorf("Messages count = %d, want 2", len(req.Messages))
	}
	if len(req.Stop) != 1 {
		t.Errorf("Stop count = %d, want 1", len(req.Stop))
	}
	if req.Extra["key"] != "value" {
		t.Error("Extra key not set")
	}
}

func TestResponse_Struct(t *testing.T) {
	resp := &Response{
		ID:           "resp-1",
		Model:        "gpt-4o",
		Content:      "Hello!",
		FinishReason: "stop",
		Usage:        Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		Provider:     "openai",
	}

	if resp.ID != "resp-1" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d", resp.Usage.TotalTokens)
	}
}

func TestCostEntry_Struct(t *testing.T) {
	entry := CostEntry{
		InputCostPer1K:  0.003,
		OutputCostPer1K: 0.015,
	}
	if entry.InputCostPer1K != 0.003 {
		t.Errorf("InputCostPer1K = %f", entry.InputCostPer1K)
	}
	if entry.OutputCostPer1K != 0.015 {
		t.Errorf("OutputCostPer1K = %f", entry.OutputCostPer1K)
	}
}
