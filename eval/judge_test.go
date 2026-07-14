package eval

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	llmtrace "github.com/atop0914/llmtrace"
)

// mockJudgeProvider is a test provider that returns pre-configured judge responses.
type mockJudgeProvider struct {
	name         string
	defaultModel string
	response     string
	err          error
	capturedReq  *llmtrace.Request
}

func (m *mockJudgeProvider) Name() string             { return m.name }
func (m *mockJudgeProvider) DefaultModel() string      { return m.defaultModel }
func (m *mockJudgeProvider) SupportsStreaming() bool    { return false }
func (m *mockJudgeProvider) Stream(_ context.Context, _ *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (m *mockJudgeProvider) Complete(_ context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	m.capturedReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &llmtrace.Response{
		ID:      "judge-resp-1",
		Content: m.response,
		Model:   m.defaultModel,
	}, nil
}

// --- Name ---

func TestJudge_Name(t *testing.T) {
	j := NewJudge(&mockJudgeProvider{},
		WithCriteria(Relevance, Coherence, Helpfulness),
	)
	name := j.Name()
	if !strings.Contains(name, "judge") {
		t.Errorf("expected name to contain 'judge', got %q", name)
	}
	if !strings.Contains(name, "relevance") {
		t.Errorf("expected name to contain 'relevance', got %q", name)
	}
	if !strings.Contains(name, "coherence") {
		t.Errorf("expected name to contain 'coherence', got %q", name)
	}
}

// --- JSON output parsing ---

func TestJudge_Eval_JSONOutput(t *testing.T) {
	judgeResp := `{"scores": {"relevance": 5, "coherence": 4, "helpfulness": 3}, "reasoning": "Good response overall."}`
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o",
		response:     judgeResp,
	}

	j := NewJudge(provider, WithCriteria(Relevance, Coherence, Helpfulness))
	result := j.Eval(context.Background(), testReq, successResp("The answer is 42."))

	if !result.Passed {
		t.Errorf("expected pass (avg=4.0, threshold=3.0), got fail: %s", result.Message)
	}
	if result.Score <= 0 || result.Score > 1.0 {
		t.Errorf("expected normalized score 0-1, got %f", result.Score)
	}
	if !strings.Contains(result.Message, "relevance: 5") {
		t.Errorf("expected relevance score in message, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "Good response") {
		t.Errorf("expected reasoning in message, got: %s", result.Message)
	}
}

func TestJudge_Eval_JSONInCodeBlock(t *testing.T) {
	judgeResp := "Here is my evaluation:\n```json\n{\"scores\": {\"relevance\": 5, \"coherence\": 5}, \"reasoning\": \"Excellent.\"}\n```"
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o",
		response:     judgeResp,
	}

	j := NewJudge(provider, WithCriteria(Relevance, Coherence))
	result := j.Eval(context.Background(), testReq, successResp("test"))

	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Message)
	}
	if result.Score < 0.99 { // avg 5/5 = 1.0
		t.Errorf("expected score ~1.0, got %f", result.Score)
	}
}

func TestJudge_Eval_LowScore(t *testing.T) {
	judgeResp := `{"scores": {"relevance": 1, "coherence": 2}, "reasoning": "Off-topic response."}`
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o",
		response:     judgeResp,
	}

	j := NewJudge(provider,
		WithCriteria(Relevance, Coherence),
		WithPassThreshold(3.0),
	)
	result := j.Eval(context.Background(), testReq, successResp("random text"))

	if result.Passed {
		t.Errorf("expected fail (avg=1.5, threshold=3.0), got pass")
	}
	if result.Score > 0.5 {
		t.Errorf("expected low normalized score, got %f", result.Score)
	}
}

// --- Non-JSON (regex) fallback ---

func TestJudge_Eval_RegexFallback(t *testing.T) {
	judgeResp := "relevance: 4\ncoherence: 3\nThe response was relevant but could be more coherent."
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o",
		response:     judgeResp,
	}

	j := NewJudge(provider,
		WithCriteria(Relevance, Coherence),
		WithJudgeModel("gpt-4o"),
	)
	// Disable JSON output to test regex fallback
	j.config.JSONOutput = false

	result := j.Eval(context.Background(), testReq, successResp("test"))

	if !result.Passed {
		t.Errorf("expected pass (avg=3.5), got fail: %s", result.Message)
	}
}

// --- Custom prompt ---

func TestJudge_Eval_CustomPrompt(t *testing.T) {
	judgeResp := `{"scores": {"quality": 5}, "reasoning": "Perfect."}`
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o",
		response:     judgeResp,
	}

	customPrompt := "Evaluate this response:\nQuestion: {{.Question}}\nResponse: {{.Response}}\nRate quality 1-5."
	customCriterion := Criterion{
		Name:        "quality",
		Description: "Overall quality",
		Rubric:      "1-5 scale",
	}

	j := NewJudge(provider,
		WithCriteria(customCriterion),
		WithCustomPrompt(customPrompt),
	)

	result := j.Eval(context.Background(), testReq, successResp("Great answer!"))
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Message)
	}

	// Verify the custom prompt was used
	if provider.capturedReq == nil {
		t.Fatal("expected captured request")
	}
	msgContent := provider.capturedReq.Messages[0].Content
	if !strings.Contains(msgContent, "Evaluate this response") {
		t.Errorf("expected custom prompt, got: %s", msgContent)
	}
	if !strings.Contains(msgContent, "Hello") {
		t.Errorf("expected question placeholder replaced, got: %s", msgContent)
	}
	if !strings.Contains(msgContent, "Great answer!") {
		t.Errorf("expected response placeholder replaced, got: %s", msgContent)
	}
}

// --- Nil response ---

func TestJudge_Eval_NilResponse(t *testing.T) {
	j := NewJudge(&mockJudgeProvider{})
	result := j.Eval(context.Background(), testReq, nil)

	if result.Passed {
		t.Error("expected fail for nil response")
	}
	if !strings.Contains(result.Message, "nil") {
		t.Errorf("expected nil message, got: %s", result.Message)
	}
}

// --- Provider error ---

func TestJudge_Eval_ProviderError(t *testing.T) {
	provider := &mockJudgeProvider{
		err: errors.New("rate limited"),
	}

	j := NewJudge(provider)
	result := j.Eval(context.Background(), testReq, successResp("test"))

	if result.Passed {
		t.Error("expected fail on provider error")
	}
	if !strings.Contains(result.Message, "rate limited") {
		t.Errorf("expected error in message, got: %s", result.Message)
	}
}

// --- Parse failures ---

func TestJudge_Eval_InvalidJSON(t *testing.T) {
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o",
		response:     "not valid json and no scores",
	}

	j := NewJudge(provider, WithCriteria(Relevance))
	result := j.Eval(context.Background(), testReq, successResp("test"))

	if result.Passed {
		t.Error("expected fail for unparseable response")
	}
	if !strings.Contains(result.Message, "failed to parse") {
		t.Errorf("expected parse error in message, got: %s", result.Message)
	}
}

// --- Model configuration ---

func TestJudge_WithJudgeModel(t *testing.T) {
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o",
		response:     `{"scores": {"relevance": 5}, "reasoning": "good"}`,
	}

	j := NewJudge(provider,
		WithCriteria(Relevance),
		WithJudgeModel("claude-3-opus"),
	)
	j.Eval(context.Background(), testReq, successResp("test"))

	if provider.capturedReq.Model != "claude-3-opus" {
		t.Errorf("expected model 'claude-3-opus', got %q", provider.capturedReq.Model)
	}
}

func TestJudge_DefaultModel(t *testing.T) {
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o-mini",
		response:     `{"scores": {"relevance": 4}, "reasoning": "ok"}`,
	}

	j := NewJudge(provider, WithCriteria(Relevance))
	j.Eval(context.Background(), testReq, successResp("test"))

	if provider.capturedReq.Model != "" {
		t.Errorf("expected empty model (use provider default), got %q", provider.capturedReq.Model)
	}
}

// --- PassThreshold ---

func TestJudge_CustomPassThreshold(t *testing.T) {
	provider := &mockJudgeProvider{
		response: `{"scores": {"relevance": 4}, "reasoning": "good"}`,
	}

	// Threshold 5.0 means avg 4.0 should fail
	j := NewJudge(provider, WithCriteria(Relevance), WithPassThreshold(5.0))
	result := j.Eval(context.Background(), testReq, successResp("test"))

	if result.Passed {
		t.Error("expected fail with threshold 5.0 and score 4")
	}
}

// --- Convenience constructors ---

func TestNewRelevanceJudge(t *testing.T) {
	j := NewRelevanceJudge(&mockJudgeProvider{})
	if !strings.Contains(j.Name(), "relevance") {
		t.Errorf("expected 'relevance' in name, got %q", j.Name())
	}
	if len(j.config.Criteria) != 1 {
		t.Errorf("expected 1 criterion, got %d", len(j.config.Criteria))
	}
}

func TestNewQualityJudge(t *testing.T) {
	j := NewQualityJudge(&mockJudgeProvider{})
	if len(j.config.Criteria) != 3 {
		t.Errorf("expected 3 criteria, got %d", len(j.config.Criteria))
	}
}

func TestNewSafetyJudge(t *testing.T) {
	j := NewSafetyJudge(&mockJudgeProvider{})
	if len(j.config.Criteria) != 1 {
		t.Errorf("expected 1 criterion, got %d", len(j.config.Criteria))
	}
	if j.config.Criteria[0].Name != "toxicity" {
		t.Errorf("expected toxicity criterion, got %q", j.config.Criteria[0].Name)
	}
}

func TestNewFactualityJudge(t *testing.T) {
	j := NewFactualityJudge(&mockJudgeProvider{})
	if j.config.Criteria[0].Name != "factuality" {
		t.Errorf("expected factuality criterion, got %q", j.config.Criteria[0].Name)
	}
}

// --- Suite integration ---

func TestJudge_InSuite(t *testing.T) {
	judgeResp := `{"scores": {"relevance": 5, "coherence": 4}, "reasoning": "Good."}`
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o",
		response:     judgeResp,
	}

	suite := NewSuite("quality",
		NonEmpty(),
		MinLength(1),
		NewJudge(provider, WithCriteria(Relevance, Coherence)),
	)

	result := suite.Run(context.Background(), testReq, successResp("The answer is 42."))
	if !result.Passed {
		t.Errorf("expected suite to pass, got: %+v", result)
	}
	if result.Total != 3 {
		t.Errorf("expected 3 evaluators, got %d", result.Total)
	}
}

func TestJudge_SuiteValidate(t *testing.T) {
	judgeResp := `{"scores": {"relevance": 1}, "reasoning": "Terrible."}`
	provider := &mockJudgeProvider{
		response: judgeResp,
	}

	suite := NewSuite("strict",
		NonEmpty(),
		NewJudge(provider, WithCriteria(Relevance), WithPassThreshold(3.0)),
	)

	_, err := suite.Validate(context.Background(), testReq, successResp("test"))
	if err == nil {
		t.Error("expected validation error when judge fails")
	}

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Error("expected ValidationError type")
	}
}

// --- Benchmark ---

func BenchmarkJudge_Eval(b *testing.B) {
	judgeResp := `{"scores": {"relevance": 5, "coherence": 4, "helpfulness": 3}, "reasoning": "Good."}`
	provider := &mockJudgeProvider{
		defaultModel: "gpt-4o",
		response:     judgeResp,
	}

	j := NewJudge(provider, WithCriteria(Relevance, Coherence, Helpfulness))
	ctx := context.Background()
	resp := successResp("The answer is 42 and here's why...")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = j.Eval(ctx, testReq, resp)
	}
}

// --- Score normalization ---

func TestJudge_ScoreNormalization(t *testing.T) {
	// MaxScore=10, score=5 should be 0.5 normalized
	judgeResp := `{"scores": {"quality": 5}, "reasoning": "ok"}`
	provider := &mockJudgeProvider{response: judgeResp}

	criterion := Criterion{Name: "quality", Description: "Quality", Rubric: "1-10"}
	j := NewJudge(provider,
		WithCriteria(criterion),
		WithMaxScore(10),
		WithPassThreshold(4.0),
	)

	result := j.Eval(context.Background(), testReq, successResp("test"))
	if result.Score < 0.49 || result.Score > 0.51 {
		t.Errorf("expected normalized score ~0.5, got %f", result.Score)
	}
}

// --- Helper: parseJSONScores ---

func TestParseJSONScores(t *testing.T) {
	raw := `{"scores": {"relevance": 5, "coherence": 3}, "reasoning": "test"}`
	scores, reasoning, err := parseJSONScores(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scores["relevance"] != 5 {
		t.Errorf("expected relevance=5, got %d", scores["relevance"])
	}
	if scores["coherence"] != 3 {
		t.Errorf("expected coherence=3, got %d", scores["coherence"])
	}
	if reasoning != "test" {
		t.Errorf("expected reasoning 'test', got %q", reasoning)
	}
}

func TestParseJSONScores_EmptyScores(t *testing.T) {
	raw := `{"scores": {}, "reasoning": "none"}`
	_, _, err := parseJSONScores(raw)
	if err == nil {
		t.Error("expected error for empty scores")
	}
}

// --- Helper: extractJSONBlock ---

func TestExtractJSONBlock_CodeFence(t *testing.T) {
	raw := "Here's the result:\n```json\n{\"scores\": {\"a\": 1}}\n```\nDone."
	block := extractJSONBlock(raw)
	if block != `{"scores": {"a": 1}}` {
		t.Errorf("unexpected block: %q", block)
	}
}

func TestExtractJSONBlock_RawJSON(t *testing.T) {
	raw := "The result is {\"scores\": {\"a\": 5}} as expected."
	block := extractJSONBlock(raw)
	if !strings.Contains(block, `"scores"`) {
		t.Errorf("expected JSON object, got %q", block)
	}
}

func TestExtractJSONBlock_NoJSON(t *testing.T) {
	raw := "No JSON here at all."
	block := extractJSONBlock(raw)
	if block != "" {
		t.Errorf("expected empty, got %q", block)
	}
}

// --- Helper: averageScore ---

func TestAverageScore(t *testing.T) {
	scores := map[string]int{"a": 4, "b": 2, "c": 5}
	avg := averageScore(scores)
	expected := 11.0 / 3.0
	if diff := avg - expected; diff > 0.001 || diff < -0.001 {
		t.Errorf("expected %f, got %f", expected, avg)
	}
}

func TestAverageScore_Empty(t *testing.T) {
	avg := averageScore(map[string]int{})
	if avg != 0 {
		t.Errorf("expected 0 for empty scores, got %f", avg)
	}
}

// --- Helper: parseRegexScores ---

func TestParseRegexScores(t *testing.T) {
	raw := "relevance: 4\ncoherence = 3\nhelpfulness: 5\n\nOverall good."
	criteria := []Criterion{Relevance, Coherence, Helpfulness}
	scores, _, err := parseRegexScores(raw, criteria)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scores["relevance"] != 4 {
		t.Errorf("expected relevance=4, got %d", scores["relevance"])
	}
	if scores["coherence"] != 3 {
		t.Errorf("expected coherence=3, got %d", scores["coherence"])
	}
	if scores["helpfulness"] != 5 {
		t.Errorf("expected helpfulness=5, got %d", scores["helpfulness"])
	}
}

func TestParseRegexScores_NoMatches(t *testing.T) {
	raw := "No scores at all."
	_, _, err := parseRegexScores(raw, []Criterion{Relevance})
	if err == nil {
		t.Error("expected error for no matches")
	}
}

// --- Criterion ---

func TestPrebuiltCriteria(t *testing.T) {
	criteria := []Criterion{Relevance, Coherence, Helpfulness, Toxicity, Factuality, InstructionFollowing}
	names := map[string]bool{}
	for _, c := range criteria {
		if c.Name == "" {
			t.Error("empty criterion name")
		}
		if c.Rubric == "" {
			t.Errorf("empty rubric for %s", c.Name)
		}
		if names[c.Name] {
			t.Errorf("duplicate criterion name: %s", c.Name)
		}
		names[c.Name] = true
	}
}

// --- JSON roundtrip for Result ---

func TestResult_JSON(t *testing.T) {
	r := Result{
		Name:    "test",
		Passed:  true,
		Score:   0.8,
		Message: "good",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var roundtrip Result
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if roundtrip.Name != r.Name || roundtrip.Passed != r.Passed || roundtrip.Score != r.Score {
		t.Errorf("JSON roundtrip mismatch: %+v vs %+v", r, roundtrip)
	}
}
