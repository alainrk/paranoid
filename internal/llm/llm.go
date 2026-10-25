// Package llm holds the clients for the online `paranoid formal`
// subcommands. It talks to LLM providers over net/http only, with no
// provider SDKs. Nothing outside the online formal subcommands may
// import this package: `paranoid verify` and every other offline
// command never call a model (see "Offline and online" in AGENTS.md).
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Message is one turn of a conversation. Role is "user" or "assistant".
type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Request is a provider-neutral completion request.
type Request struct {
	// System is the system prompt. It may be empty.
	System string
	// Messages is the conversation so far. It must not be empty.
	Messages []Message
	// MaxTokens caps the output. Zero means DefaultMaxTokens.
	MaxTokens int
}

// Response is a provider-neutral completion result.
type Response struct {
	// Text is the model's answer. It can be empty when the provider
	// refused or blocked the request; check StopReason.
	Text string
	// Model is the model name the provider reports.
	Model string
	// StopReason is the provider's own stop reason, unchanged.
	StopReason string
	// InputTokens and OutputTokens are the provider's usage counts.
	InputTokens  int
	OutputTokens int
}

// Provider is one LLM backend.
type Provider interface {
	// Name is the provider name: "anthropic", "openai", or "gemini".
	Name() string
	// Complete sends one request and returns the answer.
	Complete(ctx context.Context, req Request) (Response, error)
}

// DefaultMaxTokens is the output cap used when a request sets none.
const DefaultMaxTokens = 16000

// Provider names.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderGemini    = "gemini"
)

// DefaultAnthropicModel is the model used when the provider is
// anthropic and no model is configured.
const DefaultAnthropicModel = "claude-opus-5-5"

// ErrMalformedResponse marks a provider reply that is not the JSON
// shape the client expects.
var ErrMalformedResponse = errors.New("malformed response from provider")

// APIError is a non-success HTTP answer from a provider, after any
// retries. Message comes from the provider's error body and is
// shortened. It never contains the API key.
type APIError struct {
	Provider string
	Status   int
	Message  string
}

// Error describes the failure in one line.
func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "no error message in the reply"
	}
	return fmt.Sprintf("%s returned HTTP %d: %s", e.Provider, e.Status, msg)
}

// IsAuth reports whether the status means the key was refused.
func (e *APIError) IsAuth() bool { return e.Status == 401 || e.Status == 403 }

// validateRequest checks a request and returns the output cap to use.
func validateRequest(req Request) (int, error) {
	if len(req.Messages) == 0 {
		return 0, errors.New("request has no messages")
	}
	for i, m := range req.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return 0, fmt.Errorf("message %d has role %q, want user or assistant", i, m.Role)
		}
	}
	if req.Messages[0].Role != "user" {
		return 0, errors.New("the first message must have the user role")
	}
	if req.MaxTokens < 0 {
		return 0, errors.New("max tokens is negative")
	}
	if req.MaxTokens == 0 {
		return DefaultMaxTokens, nil
	}
	return req.MaxTokens, nil
}

// shorten cuts s to at most n bytes on a rune boundary and flattens
// white space, for error messages.
func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "..."
}
