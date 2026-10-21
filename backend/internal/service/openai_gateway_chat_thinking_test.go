package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardAsChatCompletions_PreservesThinkingChoiceInResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, choice := range []struct {
			thinking string
			effort   string
			want     string
		}{
			{thinking: "disabled", want: "none"},
			{thinking: "enabled", want: "high"},
			{thinking: "enabled", effort: "none", want: "none"},
		} {
			t.Run(fmt.Sprintf("%s_effort=%s_stream=%t", choice.thinking, choice.effort, stream), func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"deepseek-v4-pro-0813","messages":[{"role":"user","content":"Return JSON"}],"thinking":{"type":%q},"reasoning_effort":%q,"max_tokens":6500,"response_format":{"type":"json_object"},"stream":%t}`, choice.thinking, choice.effort, stream))
				response := "data: " + `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"{}"}` + "\n\n" + "data: " + `{"type":"response.completed","response":{"id":"resp_thinking","model":"deepseek-v4-pro-0813","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{}"}]}],"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\n"
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response))}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				account := &Account{ID: 150, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
					Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
					Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://upstream.example"}}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

				result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "/v1/responses", upstream.lastReq.URL.Path)
				require.Equal(t, choice.want, gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
				require.False(t, gjson.GetBytes(upstream.lastBody, "thinking").Exists())
				require.Equal(t, int64(6500), gjson.GetBytes(upstream.lastBody, "max_output_tokens").Int())
				require.Equal(t, "json_object", gjson.GetBytes(upstream.lastBody, "text.format.type").String())
				if stream {
					require.Contains(t, rec.Body.String(), `"content":"{}"`)
				} else {
					require.Equal(t, "{}", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
				}
			})
		}
	}
}
