package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatCompletionsToResponses_ThinkingMapping(t *testing.T) {
	tests := []struct {
		name          string
		request       string
		wantReasoning bool
		wantEffort    string
	}{
		{name: "disabled", request: `{"thinking":{"type":"disabled"}}`, wantReasoning: true, wantEffort: "none"},
		{name: "disabled overrides positive effort", request: `{"thinking":{"type":"disabled"},"reasoning_effort":"high"}`, wantReasoning: true, wantEffort: "none"},
		{name: "enabled uses upstream default", request: `{"thinking":{"type":"enabled"}}`, wantReasoning: false},
		{name: "enabled preserves explicit effort", request: `{"thinking":{"type":"enabled"},"reasoning_effort":"low"}`, wantReasoning: true, wantEffort: "low"},
		{name: "explicit none stays none", request: `{"thinking":{"type":"enabled"},"reasoning_effort":"none"}`, wantReasoning: true, wantEffort: "none"},
		{name: "effort without thinking", request: `{"reasoning_effort":"medium"}`, wantReasoning: true, wantEffort: "medium"},
		{name: "thinking omitted", request: `{}`, wantReasoning: false},
		{name: "unsupported boolean remains ignored", request: `{"thinking":false}`, wantReasoning: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req ChatCompletionsRequest
			body := `{"model":"deepseek-v4-pro-0813","messages":[{"role":"user","content":"hi"}]}`
			if tt.request != `{}` {
				body = body[:len(body)-1] + `,` + tt.request[1:]
			}
			require.NoError(t, json.Unmarshal([]byte(body), &req))
			resp, err := ChatCompletionsToResponses(&req)
			require.NoError(t, err)
			if !tt.wantReasoning {
				require.Nil(t, resp.Reasoning)
				return
			}
			require.NotNil(t, resp.Reasoning)
			require.Equal(t, tt.wantEffort, resp.Reasoning.Effort)
			require.Equal(t, "auto", resp.Reasoning.Summary)
		})
	}
}
