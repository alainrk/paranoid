package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const geminiDefaultBase = "https://generativelanguage.googleapis.com"

// Gemini is the client for the Gemini generateContent API.
type Gemini struct {
	opts Options
	post *poster
}

// NewGemini builds a Gemini client. Model has no default.
func NewGemini(o Options) *Gemini {
	if o.BaseURL == "" {
		o.BaseURL = geminiDefaultBase
	}
	return &Gemini{opts: o, post: newPoster(ProviderGemini, o)}
}

// Name returns "gemini".
func (g *Gemini) Name() string { return ProviderGemini }

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  struct {
		MaxOutputTokens int `json:"maxOutputTokens"`
	} `json:"generationConfig"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

// Complete sends one generateContent request. Thinking tokens count as
// output tokens, because they are billed that way.
func (g *Gemini) Complete(ctx context.Context, req Request) (Response, error) {
	maxTokens, err := validateRequest(req)
	if err != nil {
		return Response{}, fmt.Errorf("gemini: %w", err)
	}
	var body geminiRequest
	body.GenerationConfig.MaxOutputTokens = maxTokens
	if req.System != "" {
		body.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: req.System}}}
	}
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		body.Contents = append(body.Contents, geminiContent{Role: role, Parts: []geminiPart{{Text: m.Text}}})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("gemini: encoding request: %w", err)
	}
	model := strings.TrimPrefix(g.opts.Model, "models/")
	endpoint := strings.TrimRight(g.opts.BaseURL, "/") + "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	header := map[string]string{"x-goog-api-key": g.opts.APIKey}
	data, err := g.post.post(ctx, endpoint, header, raw, geminiErrorMessage)
	if err != nil {
		return Response{}, err
	}
	var out geminiResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Response{}, fmt.Errorf("gemini: reading reply (%s): %w", shorten(err.Error(), 120), ErrMalformedResponse)
	}
	resp := Response{
		Model:        out.ModelVersion,
		InputTokens:  out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount + out.UsageMetadata.ThoughtsTokenCount,
	}
	if resp.Model == "" {
		resp.Model = model
	}
	if len(out.Candidates) == 0 {
		if out.PromptFeedback.BlockReason == "" {
			return Response{}, fmt.Errorf("gemini: reply has no candidates: %w", ErrMalformedResponse)
		}
		resp.StopReason = "blocked: " + out.PromptFeedback.BlockReason
		return resp, nil
	}
	c := out.Candidates[0]
	var text strings.Builder
	for _, p := range c.Content.Parts {
		text.WriteString(p.Text)
	}
	resp.Text = text.String()
	resp.StopReason = c.FinishReason
	return resp, nil
}

// geminiErrorMessage reads {"error":{"code":..,"message":..,"status":..}}.
func geminiErrorMessage(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		if e.Error.Status != "" {
			return e.Error.Status + ": " + e.Error.Message
		}
		return e.Error.Message
	}
	return string(body)
}
