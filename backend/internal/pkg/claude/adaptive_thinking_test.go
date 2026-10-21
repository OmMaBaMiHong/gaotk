package claude

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSupportsAdaptiveThinkingUsesConfirmedModelsRatherThanEffortSupport(t *testing.T) {
	for _, model := range []string{
		"claude-opus-4-6", "anthropic/claude-sonnet-4-6", "us.anthropic.claude-opus-4-6-v1", "us.anthropic.claude-opus-4-7-v1",
		"claude-opus-4-6-20260205-thinking", "claude-opus-4-7", "claude-opus-4-8", "claude-opus-5", "claude-sonnet-5",
		"anthropic/claude-opus-5.5", "global.anthropic.claude-sonnet-5-5", "claude-haiku-5-5",
		"claude-fable-5", "claude-fable-5-1", "claude-mythos-5", "claude-mythos-5-1", "claude-mythos-preview",
	} {
		require.True(t, SupportsAdaptiveThinking(model), model)
	}
	require.NotEmpty(t, EffortLevelsForModel("claude-opus-4-5"))
	for _, model := range []string{
		"claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5", "claude-opus-4-1", "claude-sonnet-4-7",
		"claude-opus-5-6", "claude-opus-5-5-preview", "claude-opus-6", "custom-model", "gpt-5.4",
	} {
		require.False(t, SupportsAdaptiveThinking(model), model)
	}
}
