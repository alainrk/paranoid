package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const openAIDefaultBase = "https://api.openai.com/v1"

// OpenAI is the client for any OpenAI-compatible /chat/completions
// endpoint, including local servers such as Ollama or vLLM.
type OpenAI struct {
	opts Options
	post *poster
}

// NewOpenAI builds an OpenAI-compatible client. An empty BaseURL means
// the OpenAI API. Model has no default.
func NewOpenAI(o Options) *OpenAI {
	if o.BaseURL == "" {
		o.BaseURL = openAIDefaultBase
	}
	return &OpenAI{opts: o, post: newPoster(ProviderOpenAI, o)}
}

// Name returns "openai".
func (o *OpenAI) Name() string { return ProviderOpenAI }

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model               string          `json:"model"`
	Messages            []openAIMessage `json:"messages"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
}

type openAIResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Complete sends one chat completion request.
func (o *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	maxTokens, err := validateRequest(req)
	if err != nil {
		return Response{}, fmt.Errorf("openai: %w", err)
	}
	body := openAIRequest{Model: o.opts.Model}
	// The OpenAI API itself wants max_completion_tokens (max_tokens is
	// deprecated and fails on reasoning models). Compatible local
	// servers still expect max_tokens.
	if isOpenAIHost(o.opts.BaseURL) {
		body.MaxCompletionTokens = maxTokens
	} else {
		body.MaxTokens = maxTokens
	}
	if req.System != "" {
		body.Messages = append(body.Messages, openAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, openAIMessage{Role: m.Role, Content: m.Text})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("openai: encoding request: %w", err)
	}
	endpoint := strings.TrimRight(o.opts.BaseURL, "/") + "/chat/completions"
	header := map[string]string{"Authorization": "Bearer " + o.opts.APIKey}
	data, err := o.post.post(ctx, endpoint, header, raw, openAIErrorMessage)
	if err != nil {
		return Response{}, err
	}
	var out openAIResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Response{}, fmt.Errorf("openai: reading reply (%s): %w", shorten(err.Error(), 120), ErrMalformedResponse)
	}
	if len(out.Choices) == 0 {
		return Response{}, fmt.Errorf("openai: reply has no choices: %w", ErrMalformedResponse)
	}
	c := out.Choices[0]
	return Response{
		Text:         c.Message.Content,
		Model:        out.Model,
		StopReason:   c.FinishReason,
		InputTokens:  out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
	}, nil
}

// isOpenAIHost reports whether base points at api.openai.com.
func isOpenAIHost(base string) bool {
	u, err := url.Parse(base)
	return err == nil && u.Hostname() == "api.openai.com"
}

// openAIErrorMessage reads {"error":{"message":...}}.
func openAIErrorMessage(body []byte) string {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return string(body)
}
