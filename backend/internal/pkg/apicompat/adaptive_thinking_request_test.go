package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesConfirmedAdaptiveModelsNeverInventManualBudget(t *testing.T) {
	for _, model := range []string{
		"claude-opus-4-6", "anthropic/claude-sonnet-4-6", "claude-opus-4-7", "claude-opus-4-8",
		"claude-opus-5", "claude-sonnet-5", "claude-fable-5", "claude-fable-5-1", "claude-mythos-5", "claude-mythos-5-1", "claude-mythos-preview",
		"claude-opus-5-5", "anthropic/claude-sonnet-5.5", "claude-haiku-5-5",
		"us.anthropic.claude-opus-4-6-v1", "claude-opus-4-6-20260205-thinking",
	} {
		for _, effort := range []string{"low", "high"} {
			t.Run(model+"/"+effort, func(t *testing.T) {
				out, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: model, Input: json.RawMessage(`"write"`), Reasoning: &ResponsesReasoning{Effort: effort}})
				require.NoError(t, err)
				require.Equal(t, "adaptive", out.Thinking.Type)
				require.Zero(t, out.Thinking.BudgetTokens)
				require.Zero(t, out.MaxTokens)
				require.Equal(t, effort, out.OutputConfig.Effort)
			})
		}
	}
}

func TestResponsesAdaptiveSelectionPreservesExplicitThinkingAndLegacyBoundary(t *testing.T) {
	for _, model := range []string{"claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8", "claude-sonnet-5", "claude-opus-4-5"} {
		cc := &ChatCompletionsRequest{Model: model, Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"write"`)}}, ReasoningEffort: "low", Thinking: json.RawMessage(`{"type":"enabled","budget_tokens":2048}`)}
		intermediate, err := ChatCompletionsToResponses(cc)
		require.NoError(t, err)
		out, err := ResponsesToAnthropicRequest(intermediate)
		require.NoError(t, err)
		require.Equal(t, "enabled", out.Thinking.Type, model)
		require.Equal(t, 2048, out.Thinking.BudgetTokens, model)
	}
	for _, model := range []string{"claude-opus-4-5", "claude-sonnet-4-5", "custom-model", "claude-opus-6", "claude-sonnet-4-7"} {
		out, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: model, Input: json.RawMessage(`"write"`), Reasoning: &ResponsesReasoning{Effort: "high"}})
		require.NoError(t, err)
		require.Equal(t, "enabled", out.Thinking.Type, model)
		require.Zero(t, out.Thinking.BudgetTokens, "legacy manual mode still requires explicit caller/provider configuration")
	}
}

func TestResponsesAdaptiveSelectionDoesNotTurnOnAnUnrequestedMode(t *testing.T) {
	for _, model := range []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-opus-4-8"} {
		out, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: model, Input: json.RawMessage(`"write"`)})
		require.NoError(t, err)
		require.Nil(t, out.Thinking)
		out, err = ResponsesToAnthropicRequest(&ResponsesRequest{Model: model, Input: json.RawMessage(`"write"`), Reasoning: &ResponsesReasoning{Effort: "none"}})
		require.NoError(t, err)
		require.Equal(t, "disabled", out.Thinking.Type)
	}
}
