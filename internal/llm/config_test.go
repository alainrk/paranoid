package llm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/policy"
)

func TestResolvePrecedence(t *testing.T) {
	file := policy.Formal{
		LLM:   policy.LLM{Provider: "anthropic", Model: "claude-default", APIKeyEnv: "TEAM_ANTHROPIC"},
		Proof: policy.LLM{Provider: "openai", Model: "local-prover", BaseURL: "http://localhost:11434/v1", APIKeyEnv: "LOCAL_LLM_KEY"},
		Spec:  policy.LLM{Provider: "gemini", Model: "gemini-pro"},
	}
	tests := []struct {
		name    string
		task    Task
		formal  policy.Formal
		flags   Flags
		want    Settings
		wantErr string
	}{
		{
			name: "nothing set gives the defaults",
			task: TaskProof,
			want: Settings{Task: TaskProof, Provider: "anthropic", Model: "claude-opus-5-5", BaseURL: "https://api.anthropic.com", APIKeyEnv: "ANTHROPIC_API_KEY"},
		},
		{
			name:   "default table applies to a task with no table",
			task:   TaskSpec,
			formal: policy.Formal{LLM: policy.LLM{Provider: "anthropic", Model: "claude-default"}},
			want:   Settings{Task: TaskSpec, Provider: "anthropic", Model: "claude-default", BaseURL: "https://api.anthropic.com", APIKeyEnv: "ANTHROPIC_API_KEY"},
		},
		{
			name:   "task table wins and does not inherit from another provider",
			task:   TaskProof,
			formal: file,
			want:   Settings{Task: TaskProof, Provider: "openai", Model: "local-prover", BaseURL: "http://localhost:11434/v1", APIKeyEnv: "LOCAL_LLM_KEY"},
		},
		{
			name:   "spec task uses gemini defaults for url and key",
			task:   TaskSpec,
			formal: file,
			want:   Settings{Task: TaskSpec, Provider: "gemini", Model: "gemini-pro", BaseURL: "https://generativelanguage.googleapis.com", APIKeyEnv: "GEMINI_API_KEY"},
		},
		{
			name:   "task table without provider inherits from the default table",
			task:   TaskProof,
			formal: policy.Formal{LLM: policy.LLM{Provider: "anthropic", Model: "m1", APIKeyEnv: "K1"}, Proof: policy.LLM{Model: "m2"}},
			want:   Settings{Task: TaskProof, Provider: "anthropic", Model: "m2", BaseURL: "https://api.anthropic.com", APIKeyEnv: "K1"},
		},
		{
			name:   "flags beat the file",
			task:   TaskProof,
			formal: file,
			flags:  Flags{Model: "other-model", BaseURL: "http://127.0.0.1:8000/v1"},
			want:   Settings{Task: TaskProof, Provider: "openai", Model: "other-model", BaseURL: "http://127.0.0.1:8000/v1", APIKeyEnv: "LOCAL_LLM_KEY"},
		},
		{
			name:   "provider flag drops model and key variable of the other provider",
			task:   TaskProof,
			formal: file,
			flags:  Flags{Provider: "anthropic"},
			want:   Settings{Task: TaskProof, Provider: "anthropic", Model: "claude-default", BaseURL: "https://api.anthropic.com", APIKeyEnv: "TEAM_ANTHROPIC"},
		},
		{
			name:   "provider flag with a task that has an unnamed provider table",
			task:   TaskProof,
			formal: policy.Formal{LLM: policy.LLM{Provider: "anthropic"}, Proof: policy.LLM{Model: "claude-x"}},
			flags:  Flags{Provider: "openai", Model: "gpt-x"},
			want:   Settings{Task: TaskProof, Provider: "openai", Model: "gpt-x", BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY"},
		},
		{
			name:    "openai needs a model",
			task:    TaskProof,
			flags:   Flags{Provider: "openai"},
			wantErr: "no model is set for provider openai",
		},
		{
			name:    "unknown provider flag",
			task:    TaskProof,
			flags:   Flags{Provider: "mystery"},
			wantErr: "unknown provider",
		},
		{
			name:    "bad base url flag",
			task:    TaskProof,
			flags:   Flags{BaseURL: "localhost:11434"},
			wantErr: "http or https",
		},
		{
			name:    "base url flag with a password",
			task:    TaskProof,
			flags:   Flags{BaseURL: "https://u:p@host/v1"},
			wantErr: "user name or password",
		},
		{
			name:    "unknown task",
			task:    Task("nope"),
			wantErr: "unknown task",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.task, tt.formal, tt.flags)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseTask(t *testing.T) {
	for _, s := range []string{"proof", "spec"} {
		got, err := ParseTask(s)
		require.NoError(t, err)
		require.Equal(t, Task(s), got)
	}
	_, err := ParseTask("fill")
	require.Error(t, err)
}

func TestAPIKey(t *testing.T) {
	s := Settings{Task: TaskProof, Provider: "gemini", APIKeyEnv: "GEMINI_API_KEY"}
	env := map[string]string{"ANTHROPIC_API_KEY": "other-provider-key", "OPENAI_API_KEY": "another"}
	getenv := func(k string) string { return env[k] }

	// No silent fallback to another provider's key.
	_, err := s.APIKey(getenv)
	require.Error(t, err)
	require.Contains(t, err.Error(), "GEMINI_API_KEY")
	require.NotContains(t, err.Error(), "other-provider-key")
	require.False(t, s.KeyIsSet(getenv))

	env["GEMINI_API_KEY"] = "gem-key"
	key, err := s.APIKey(getenv)
	require.NoError(t, err)
	require.Equal(t, "gem-key", key)
	require.True(t, s.KeyIsSet(getenv))
}

func TestSettingsNewProvider(t *testing.T) {
	for _, name := range []string{"anthropic", "openai", "gemini"} {
		p, err := Settings{Provider: name, Model: "m", BaseURL: "http://x"}.NewProvider(Options{APIKey: "k"})
		require.NoError(t, err)
		require.Equal(t, name, p.Name())
	}
	_, err := Settings{Provider: "nope"}.NewProvider(Options{})
	require.Error(t, err)
}
