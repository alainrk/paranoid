package llm

import (
	"context"
	"fmt"
)

// Client is the provider commands use. It wraps a Provider with the
// run caps and the request log, so every call is counted and written
// down. It implements Provider.
type Client struct {
	provider Provider
	model    string
	budget   *Budget
	log      *RunLog
}

// NewClient composes a provider with a budget and a log. model is the
// configured model name, written to the log when the provider does not
// report one. budget and log must not be nil.
func NewClient(p Provider, model string, b *Budget, l *RunLog) *Client {
	return &Client{provider: p, model: model, budget: b, log: l}
}

// Name returns the wrapped provider's name.
func (c *Client) Name() string { return c.provider.Name() }

// Budget returns the run budget, so a command can print the usage.
func (c *Client) Budget() *Budget { return c.budget }

// Complete checks the caps, calls the provider, counts the tokens, and
// writes the log line. A cap error comes back before anything is sent.
// A failed call is logged too, with its error text.
func (c *Client) Complete(ctx context.Context, req Request) (Response, error) {
	if err := c.budget.Admit(); err != nil {
		return Response{}, err
	}
	resp, callErr := c.provider.Complete(ctx, req)
	c.budget.Record(resp.InputTokens, resp.OutputTokens)
	entry := LogEntry{
		Provider:     c.provider.Name(),
		Model:        resp.Model,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		StopReason:   resp.StopReason,
		System:       req.System,
		Messages:     req.Messages,
		MaxTokens:    req.MaxTokens,
		Response:     resp.Text,
	}
	if entry.Model == "" {
		entry.Model = c.model
	}
	if callErr != nil {
		entry.Error = callErr.Error()
	}
	if err := c.log.Append(entry); err != nil {
		return Response{}, fmt.Errorf("a request was sent but could not be logged: %w", err)
	}
	return resp, callErr
}
