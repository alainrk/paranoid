package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	anthropicDefaultBase = "https://api.anthropic.com"
	anthropicVersion     = "2023-06-01"
)

// Anthropic is the client for the Anthropic Messages API.
type Anthropic struct {
	opts Options
	post *poster
}

// NewAnthropic builds an Anthropic client. An empty Model becomes
// DefaultAnthropicModel.
func NewAnthropic(o Options) *Anthropic {
	if o.Model == "" {
		o.Model = DefaultAnthropicModel
	}
	if o.BaseURL == "" {
		o.BaseURL = anthropicDefaultBase
	}
	return &Anthropic{opts: o, post: newPoster(ProviderAnthropic, o)}
}

// Name returns "anthropic".
func (a *Anthropic) Name() string { return ProviderAnthropic }

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Type    string `json:"type"`
	Model   string `json:"model"`
	Content *[]struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Complete sends one Messages API request. Only text blocks are
// returned; thinking blocks are dropped.
func (a *Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	maxTokens, err := validateRequest(req)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: %w", err)
	}
	body := anthropicRequest{Model: a.opts.Model, MaxTokens: maxTokens, System: req.System}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, anthropicMessage{Role: m.Role, Content: m.Text})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: encoding request: %w", err)
	}
	url := strings.TrimRight(a.opts.BaseURL, "/") + "/v1/messages"
	header := map[string]string{"x-api-key": a.opts.APIKey, "anthropic-version": anthropicVersion}
	data, err := a.post.post(ctx, url, header, raw, anthropicErrorMessage)
	if err != nil {
		return Response{}, err
	}
	var out anthropicResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Response{}, fmt.Errorf("anthropic: reading reply (%s): %w", shorten(err.Error(), 120), ErrMalformedResponse)
	}
	if out.Content == nil {
		return Response{}, fmt.Errorf("anthropic: reply has no content field: %w", ErrMalformedResponse)
	}
	var text strings.Builder
	for _, b := range *out.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	return Response{
		Text:         text.String(),
		Model:        out.Model,
		StopReason:   out.StopReason,
		InputTokens:  out.Usage.InputTokens,
		OutputTokens: out.Usage.OutputTokens,
	}, nil
}

// anthropicErrorMessage reads {"type":"error","error":{"message":...}}.
func anthropicErrorMessage(body []byte) string {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		if e.Error.Type != "" {
			return e.Error.Type + ": " + e.Error.Message
		}
		return e.Error.Message
	}
	return string(body)
}
