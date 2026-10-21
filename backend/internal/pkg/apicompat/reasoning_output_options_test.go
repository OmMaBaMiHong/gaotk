package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesBridgeDoesNotInventOutputOrThinkingBudget(t *testing.T) {
	for _, effort := range []string{"", "none", "low", "high"} {
		req := &ResponsesRequest{Model: "custom-model", Input: json.RawMessage(`"write a chapter"`)}
		if effort != "" {
			req.Reasoning = &ResponsesReasoning{Effort: effort}
		}
		out, err := ResponsesToAnthropicRequest(req)
		require.NoError(t, err)
		require.Zero(t, out.MaxTokens)
		if out.Thinking != nil {
			require.Zero(t, out.Thinking.BudgetTokens)
		}
		if effort == "none" {
			require.Equal(t, "disabled", out.Thinking.Type)
		}
		if effort == "low" || effort == "high" {
			require.Equal(t, "enabled", out.Thinking.Type)
		}
	}
}

func TestChatBridgeDoesNotInventReasoningEffort(t *testing.T) {
	for _, raw := range []string{`{"type":"enabled"}`, `{"type":"adaptive"}`} {
		req := &ChatCompletionsRequest{Model: "deepseek-flash", Thinking: json.RawMessage(raw)}
		require.Empty(t, chatCompletionsReasoningEffort(req))
		req.ReasoningEffort = "low"
		require.Equal(t, "low", chatCompletionsReasoningEffort(req))
	}
}

func TestChatBridgeEnablesGPT54InsteadOfItsNoneDefault(t *testing.T) {
	for _, model := range []string{"gpt-5.4", "gpt-5.4-2026-03-05", "openai/gpt-5.4"} {
		req := &ChatCompletionsRequest{Model: model, Thinking: json.RawMessage(`{"type":"enabled"}`)}
		require.Equal(t, "medium", chatCompletionsReasoningEffort(req))
		req.ReasoningEffort = "low"
		require.Equal(t, "low", chatCompletionsReasoningEffort(req))
		req.Thinking = json.RawMessage(`{"type":"disabled"}`)
		require.Equal(t, "none", chatCompletionsReasoningEffort(req))
	}
}

func TestChatAnthropicRoundTripPreservesExplicitThinking(t *testing.T) {
	for _, mode := range []string{"enabled", "adaptive", "disabled"} {
		cc := &ChatCompletionsRequest{Model: "custom-model", Thinking: json.RawMessage(`{"type":"` + mode + `","budget_tokens":2048}`), Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"write"`)}}}
		resp, err := ChatCompletionsToResponses(cc)
		require.NoError(t, err)
		wire, err := json.Marshal(resp)
		require.NoError(t, err)
		require.NotContains(t, string(wire), "budget_tokens", "Anthropic-only options must not leak to Responses upstreams")
		out, err := ResponsesToAnthropicRequest(resp)
		require.NoError(t, err)
		require.NotNil(t, out.Thinking)
		require.Equal(t, mode, out.Thinking.Type)
		require.Equal(t, 2048, out.Thinking.BudgetTokens)
	}
}

func TestOpus55OffUsesLowCompatibility(t *testing.T) {
	out, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: "claude-opus-5-5", Input: json.RawMessage(`"write"`), Reasoning: &ResponsesReasoning{Effort: "none"}})
	require.NoError(t, err)
	require.Equal(t, "adaptive", out.Thinking.Type)
	require.Equal(t, "low", out.OutputConfig.Effort)
	require.Zero(t, out.Thinking.BudgetTokens)
}
