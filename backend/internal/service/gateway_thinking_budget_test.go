//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const basetenBudgetError = "Error from provider (Console Go): Upstream request failed: [invalid_request_error] must be greater than 1024 to reserve tokens for a final answer when Baseten reasoning is enabled"

func TestThinkingBudgetConstraintErrors(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{basetenBudgetError, true},
		{"MUST BE GREATER THAN 1024 TO RESERVE TOKENS FOR A FINAL ANSWER WHEN BASETEN REASONING IS ENABLED", true},
		{"thinking.budget_tokens must be >= 1024", true},
		{"thinking budget tokens must be greater than or equal to 1024", true},
		{"thinking.budget_tokens: Input should be greater than 1024", true},
		{"reasoning quota exceeded: 1024 tokens", false},
		{"must be greater than 1024", false},
		{"reserve tokens for a final answer when Baseten reasoning is enabled", false},
		{"must be greater than 2048 to reserve tokens for a final answer when Baseten reasoning is enabled", false},
		{"", false},
	} {
		t.Run(tc.message, func(t *testing.T) {
			require.Equal(t, tc.want, isThinkingBudgetConstraintError(tc.message))
		})
	}
}
