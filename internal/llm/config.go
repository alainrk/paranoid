package llm

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/alainrk/paranoid/internal/policy"
)

// Task names a kind of online work. Each task can use its own provider.
type Task string

// The tasks in v1.
const (
	// TaskProof is proof search (fill, prove).
	TaskProof Task = "proof"
	// TaskSpec is spec drafting and Lean models (prove).
	TaskSpec Task = "spec"
)

// ParseTask turns a flag value into a Task.
func ParseTask(s string) (Task, error) {
	switch Task(s) {
	case TaskProof, TaskSpec:
		return Task(s), nil
	}
	return "", fmt.Errorf("unknown task %q, want proof or spec", s)
}

// Flags are the command-line overrides. An empty field is not set.
type Flags struct {
	Provider string
	Model    string
	BaseURL  string
}

// Settings are the resolved, non-secret settings for one task.
type Settings struct {
	Task      Task
	Provider  string
	Model     string
	BaseURL   string
	APIKeyEnv string
}

// defaultKeyEnv is the environment variable each provider reads its key
// from unless api_key_env says otherwise.
var defaultKeyEnv = map[string]string{
	ProviderAnthropic: "ANTHROPIC_API_KEY",
	ProviderOpenAI:    "OPENAI_API_KEY",
	ProviderGemini:    "GEMINI_API_KEY",
}

// defaultBaseURL is the endpoint root used when none is configured.
var defaultBaseURL = map[string]string{
	ProviderAnthropic: anthropicDefaultBase,
	ProviderOpenAI:    openAIDefaultBase,
	ProviderGemini:    geminiDefaultBase,
}

// Resolve merges the settings for one task. Per field, the first one
// set wins, in this order: command-line flag, the task table
// ([formal.llm.proof] or [formal.llm.spec]), [formal.llm], then the
// default. A table that names a different provider than the one that
// wins does not lend its model, base URL, or key variable, because
// they belong to that other provider. The default provider is
// anthropic, with model claude-opus-5-5. Other providers have no
// default model, so a missing model is an error.
func Resolve(task Task, f policy.Formal, flags Flags) (Settings, error) {
	var taskTable policy.LLM
	switch task {
	case TaskProof:
		taskTable = f.Proof
	case TaskSpec:
		taskTable = f.Spec
	default:
		return Settings{}, fmt.Errorf("unknown task %q", task)
	}
	layers := []policy.LLM{
		{Provider: flags.Provider, Model: flags.Model, BaseURL: flags.BaseURL},
		taskTable,
		f.LLM,
	}

	s := Settings{Task: task}
	for _, l := range layers {
		if l.Provider != "" {
			s.Provider = l.Provider
			break
		}
	}
	if s.Provider == "" {
		s.Provider = ProviderAnthropic
	}
	if _, ok := defaultKeyEnv[s.Provider]; !ok {
		return Settings{}, fmt.Errorf("unknown provider %q, want anthropic, openai, or gemini", s.Provider)
	}

	// A table that names no provider belongs to the provider of the
	// table below it, or to the default. Flags always apply.
	owner := make([]string, len(layers))
	below := ProviderAnthropic
	for i := len(layers) - 1; i >= 1; i-- {
		if layers[i].Provider != "" {
			below = layers[i].Provider
		}
		owner[i] = below
	}
	pick := func(get func(policy.LLM) string) string {
		for i, l := range layers {
			if i > 0 && owner[i] != s.Provider {
				continue
			}
			if v := get(l); v != "" {
				return v
			}
		}
		return ""
	}
	s.Model = pick(func(l policy.LLM) string { return l.Model })
	s.BaseURL = pick(func(l policy.LLM) string { return l.BaseURL })
	s.APIKeyEnv = pick(func(l policy.LLM) string { return l.APIKeyEnv })

	if s.Model == "" && s.Provider == ProviderAnthropic {
		s.Model = DefaultAnthropicModel
	}
	if s.Model == "" {
		return Settings{}, fmt.Errorf("no model is set for provider %s (task %s). Set model in [formal.llm] or [formal.llm.%s], or pass --model", s.Provider, task, task)
	}
	if s.BaseURL == "" {
		s.BaseURL = defaultBaseURL[s.Provider]
	}
	u, err := url.Parse(s.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Settings{}, errors.New("the base URL must be an http or https URL")
	}
	if u.User != nil {
		return Settings{}, errors.New("the base URL must not contain a user name or password")
	}
	if s.APIKeyEnv == "" {
		s.APIKeyEnv = defaultKeyEnv[s.Provider]
	}
	return s, nil
}

// KeyIsSet reports whether the key variable has a value, without
// reading the value out.
func (s Settings) KeyIsSet(getenv func(string) string) bool {
	return getenv(s.APIKeyEnv) != ""
}

// APIKey reads the key from the environment. A missing key is an
// error that says which variable to set. There is no fallback to
// another provider or another variable.
func (s Settings) APIKey(getenv func(string) string) (string, error) {
	key := getenv(s.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("no API key for provider %s (task %s): set the %s environment variable", s.Provider, s.Task, s.APIKeyEnv)
	}
	return key, nil
}

// NewProvider builds the client for these settings. The caller reads
// the key with APIKey and passes it in opts.
func (s Settings) NewProvider(opts Options) (Provider, error) {
	opts.Model = s.Model
	opts.BaseURL = s.BaseURL
	switch s.Provider {
	case ProviderAnthropic:
		return NewAnthropic(opts), nil
	case ProviderOpenAI:
		return NewOpenAI(opts), nil
	case ProviderGemini:
		return NewGemini(opts), nil
	}
	return nil, fmt.Errorf("unknown provider %q", s.Provider)
}
