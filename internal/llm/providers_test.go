package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testKey = "sk-test-KEY-0123456789"

// step is one scripted server answer.
type step struct {
	status     int
	body       string
	retryAfter string
}

// scripted serves steps in order (the last repeats) and records what it
// was sent.
type scripted struct {
	mu      sync.Mutex
	steps   []step
	calls   int
	paths   []string
	headers []http.Header
	bodies  []string
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := io.ReadAll(r.Body)
	s.paths = append(s.paths, r.URL.Path)
	s.headers = append(s.headers, r.Header.Clone())
	s.bodies = append(s.bodies, string(b))
	st := s.steps[min(s.calls, len(s.steps)-1)]
	s.calls++
	if st.retryAfter != "" {
		w.Header().Set("Retry-After", st.retryAfter)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(st.status)
	_, _ = io.WriteString(w, st.body)
}

// sleeps records waits instead of sleeping.
type sleeps struct{ got []time.Duration }

func (s *sleeps) sleep(_ context.Context, d time.Duration) error {
	s.got = append(s.got, d)
	return nil
}

const (
	anthropicOK = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":"hmm","signature":"s"},{"type":"text","text":"theorem done"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":34}}`
	openaiOK    = `{"id":"c1","object":"chat.completion","model":"gpt-x","choices":[{"index":0,"message":{"role":"assistant","content":"theorem done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":34,"total_tokens":46}}`
	geminiOK    = `{"candidates":[{"content":{"role":"model","parts":[{"text":"theorem "},{"text":"done"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":30,"thoughtsTokenCount":4},"modelVersion":"gemini-pro-001"}`
)

type providerCase struct {
	name       string
	build      func(Options) Provider
	okBody     string
	errBody    string
	wantPath   string
	authHeader string
	authValue  string
	wantModel  string
}

func providerCases() []providerCase {
	return []providerCase{
		{
			name:       "anthropic",
			build:      func(o Options) Provider { return NewAnthropic(o) },
			okBody:     anthropicOK,
			errBody:    `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
			wantPath:   "/v1/messages",
			authHeader: "x-api-key",
			authValue:  testKey,
			wantModel:  "claude-opus-5-5",
		},
		{
			name:       "openai",
			build:      func(o Options) Provider { return NewOpenAI(o) },
			okBody:     openaiOK,
			errBody:    `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`,
			wantPath:   "/chat/completions",
			authHeader: "Authorization",
			authValue:  "Bearer " + testKey,
			wantModel:  "gpt-x",
		},
		{
			name:       "gemini",
			build:      func(o Options) Provider { return NewGemini(o) },
			okBody:     geminiOK,
			errBody:    `{"error":{"code":400,"message":"API key not valid.","status":"INVALID_ARGUMENT"}}`,
			wantPath:   "/v1beta/models/test-model:generateContent",
			authHeader: "X-Goog-Api-Key",
			authValue:  testKey,
			wantModel:  "gemini-pro-001",
		},
	}
}

func newOpts(srv *httptest.Server, sl *sleeps) Options {
	return Options{
		BaseURL: srv.URL,
		Model:   "test-model",
		APIKey:  testKey,
		Sleep:   sl.sleep,
		Now:     func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
}

var testReq = Request{
	System:    "You write Lean proofs.",
	Messages:  []Message{{Role: "user", Text: "prove it"}, {Role: "assistant", Text: "ok"}, {Role: "user", Text: "go"}},
	MaxTokens: 500,
}

func TestProvidersSuccess(t *testing.T) {
	for _, pc := range providerCases() {
		t.Run(pc.name, func(t *testing.T) {
			sc := &scripted{steps: []step{{status: 200, body: pc.okBody}}}
			srv := httptest.NewServer(sc)
			defer srv.Close()
			p := pc.build(newOpts(srv, &sleeps{}))
			require.Equal(t, pc.name, p.Name())

			resp, err := p.Complete(t.Context(), testReq)
			require.NoError(t, err)
			require.Equal(t, "theorem done", resp.Text)
			require.Equal(t, pc.wantModel, resp.Model)
			require.Equal(t, 12, resp.InputTokens)
			require.Equal(t, 34, resp.OutputTokens)
			require.NotEmpty(t, resp.StopReason)

			require.Equal(t, 1, sc.calls)
			require.Equal(t, pc.wantPath, sc.paths[0])
			require.Equal(t, pc.authValue, sc.headers[0].Get(pc.authHeader))
			require.Contains(t, sc.bodies[0], "prove it")
			require.Contains(t, sc.bodies[0], "You write Lean proofs.")
			require.NotContains(t, sc.bodies[0], testKey)
		})
	}
}

func TestAnthropicRequestShape(t *testing.T) {
	sc := &scripted{steps: []step{{status: 200, body: anthropicOK}}}
	srv := httptest.NewServer(sc)
	defer srv.Close()
	_, err := NewAnthropic(newOpts(srv, &sleeps{})).Complete(t.Context(), testReq)
	require.NoError(t, err)
	require.Equal(t, "2023-06-01", sc.headers[0].Get("anthropic-version"))
	require.JSONEq(t, `{"model":"test-model","max_tokens":500,"system":"You write Lean proofs.","messages":[{"role":"user","content":"prove it"},{"role":"assistant","content":"ok"},{"role":"user","content":"go"}]}`, sc.bodies[0])
}

func TestGeminiRequestShape(t *testing.T) {
	sc := &scripted{steps: []step{{status: 200, body: geminiOK}}}
	srv := httptest.NewServer(sc)
	defer srv.Close()
	resp, err := NewGemini(newOpts(srv, &sleeps{})).Complete(t.Context(), testReq)
	require.NoError(t, err)
	require.Equal(t, 34, resp.OutputTokens, "thinking tokens count as output")
	require.JSONEq(t, `{"systemInstruction":{"parts":[{"text":"You write Lean proofs."}]},"contents":[{"role":"user","parts":[{"text":"prove it"}]},{"role":"model","parts":[{"text":"ok"}]},{"role":"user","parts":[{"text":"go"}]}],"generationConfig":{"maxOutputTokens":500}}`, sc.bodies[0])
}

func TestOpenAITokenFieldByHost(t *testing.T) {
	sc := &scripted{steps: []step{{status: 200, body: openaiOK}}}
	srv := httptest.NewServer(sc)
	defer srv.Close()
	_, err := NewOpenAI(newOpts(srv, &sleeps{})).Complete(t.Context(), testReq)
	require.NoError(t, err)
	require.Contains(t, sc.bodies[0], `"max_tokens":500`)
	require.NotContains(t, sc.bodies[0], "max_completion_tokens")
	require.True(t, isOpenAIHost("https://api.openai.com/v1"))
	require.False(t, isOpenAIHost("http://localhost:11434/v1"))
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	for _, pc := range providerCases() {
		t.Run(pc.name, func(t *testing.T) {
			sc := &scripted{steps: []step{
				{status: 429, body: `{"error":{"message":"slow down"}}`, retryAfter: "7"},
				{status: 529, body: `{"error":{"message":"overloaded"}}`},
				{status: 200, body: pc.okBody},
			}}
			srv := httptest.NewServer(sc)
			defer srv.Close()
			sl := &sleeps{}
			resp, err := pc.build(newOpts(srv, sl)).Complete(t.Context(), testReq)
			require.NoError(t, err)
			require.Equal(t, "theorem done", resp.Text)
			require.Equal(t, 3, sc.calls)
			require.Equal(t, []time.Duration{7 * time.Second, 2 * time.Second}, sl.got)
		})
	}
}

func TestRetriesExhaustedOn500(t *testing.T) {
	for _, pc := range providerCases() {
		t.Run(pc.name, func(t *testing.T) {
			sc := &scripted{steps: []step{{status: 500, body: `{"error":{"message":"boom ` + testKey + `"}}`}}}
			srv := httptest.NewServer(sc)
			defer srv.Close()
			sl := &sleeps{}
			_, err := pc.build(newOpts(srv, sl)).Complete(t.Context(), testReq)
			require.Error(t, err)
			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			require.Equal(t, 500, apiErr.Status)
			require.Equal(t, 4, sc.calls, "one try plus three retries")
			require.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, sl.got)
			require.NotContains(t, err.Error(), testKey)
			require.Contains(t, err.Error(), "[redacted]")
		})
	}
}

func TestNoRetryOnClientErrors(t *testing.T) {
	for _, pc := range providerCases() {
		for _, status := range []int{400, 401, 403, 404} {
			t.Run(pc.name, func(t *testing.T) {
				sc := &scripted{steps: []step{{status: status, body: pc.errBody}}}
				srv := httptest.NewServer(sc)
				defer srv.Close()
				sl := &sleeps{}
				_, err := pc.build(newOpts(srv, sl)).Complete(t.Context(), testReq)
				var apiErr *APIError
				require.ErrorAs(t, err, &apiErr)
				require.Equal(t, status, apiErr.Status)
				require.Equal(t, status == 401 || status == 403, apiErr.IsAuth())
				require.Equal(t, 1, sc.calls)
				require.Empty(t, sl.got)
				require.NotEmpty(t, apiErr.Message)
				require.NotContains(t, err.Error(), testKey)
			})
		}
	}
}

func TestMalformedBodies(t *testing.T) {
	bodies := map[string][]string{
		"anthropic": {`not json`, `{"type":"message"}`, `[]`},
		"openai":    {`not json`, `{"choices":[]}`, `[]`},
		"gemini":    {`not json`, `{"usageMetadata":{}}`, `[]`},
	}
	for _, pc := range providerCases() {
		for _, body := range bodies[pc.name] {
			t.Run(pc.name+"/"+body, func(t *testing.T) {
				sc := &scripted{steps: []step{{status: 200, body: body}}}
				srv := httptest.NewServer(sc)
				defer srv.Close()
				_, err := pc.build(newOpts(srv, &sleeps{})).Complete(t.Context(), testReq)
				require.ErrorIs(t, err, ErrMalformedResponse)
				require.Equal(t, 1, sc.calls, "a malformed 200 is not retried")
			})
		}
	}
}

func TestGeminiBlockedPrompt(t *testing.T) {
	sc := &scripted{steps: []step{{status: 200, body: `{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":3}}`}}}
	srv := httptest.NewServer(sc)
	defer srv.Close()
	resp, err := NewGemini(newOpts(srv, &sleeps{})).Complete(t.Context(), testReq)
	require.NoError(t, err)
	require.Empty(t, resp.Text)
	require.Equal(t, "blocked: SAFETY", resp.StopReason)
}

func TestInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		req  Request
	}{
		{"no messages", Request{}},
		{"bad role", Request{Messages: []Message{{Role: "system", Text: "x"}}}},
		{"assistant first", Request{Messages: []Message{{Role: "assistant", Text: "x"}}}},
		{"negative cap", Request{Messages: []Message{{Role: "user", Text: "x"}}, MaxTokens: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No server: a bad request must fail before any network use.
			p := NewAnthropic(Options{BaseURL: "http://127.0.0.1:1", APIKey: testKey})
			_, err := p.Complete(t.Context(), tt.req)
			require.Error(t, err)
		})
	}
}

func TestDefaultMaxTokensAndModel(t *testing.T) {
	sc := &scripted{steps: []step{{status: 200, body: anthropicOK}}}
	srv := httptest.NewServer(sc)
	defer srv.Close()
	p := NewAnthropic(Options{BaseURL: srv.URL, APIKey: testKey})
	_, err := p.Complete(t.Context(), Request{Messages: []Message{{Role: "user", Text: "hi"}}})
	require.NoError(t, err)
	require.Contains(t, sc.bodies[0], `"model":"claude-opus-5-5"`)
	require.Contains(t, sc.bodies[0], `"max_tokens":16000`)
}

func TestContextCancelStopsRetries(t *testing.T) {
	sc := &scripted{steps: []step{{status: 429, body: `{}`}}}
	srv := httptest.NewServer(sc)
	defer srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	o := newOpts(srv, &sleeps{})
	o.Sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	_, err := NewAnthropic(o).Complete(ctx, testReq)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, sc.calls)
}

func TestConnectionErrorHidesKeyAndIsNotRetried(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	sl := &sleeps{}
	o := Options{BaseURL: url + "/" + testKey, Model: "m", APIKey: testKey, Sleep: sl.sleep}
	_, err := NewOpenAI(o).Complete(t.Context(), testReq)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testKey)
	require.Empty(t, sl.got)
	require.False(t, errors.Is(err, ErrMalformedResponse))
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"3", 3 * time.Second},
		{"0.5", 500 * time.Millisecond},
		{"-4", 0},
		{"garbage", 0},
		{now.Add(30 * time.Second).Format(http.TimeFormat), 30 * time.Second},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, parseRetryAfter(tt.in, now), tt.in)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	sc := &scripted{steps: []step{{status: 429, body: `{}`, retryAfter: "99999"}, {status: 200, body: anthropicOK}}}
	srv := httptest.NewServer(sc)
	defer srv.Close()
	sl := &sleeps{}
	_, err := NewAnthropic(newOpts(srv, sl)).Complete(t.Context(), testReq)
	require.NoError(t, err)
	require.Equal(t, []time.Duration{maxBackoff}, sl.got)
}

func newServer(h http.Handler) *httptest.Server { return httptest.NewServer(h) }
