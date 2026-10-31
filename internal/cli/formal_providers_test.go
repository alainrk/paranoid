package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormalProviders(t *testing.T) {
	policyFile := `[formal.llm]
provider = "anthropic"

[formal.llm.proof]
provider = "openai"
model = "my-local-prover"
base_url = "http://localhost:11434/v1"
api_key_env = "LOCAL_LLM_KEY"

[formal.llm.spec]
provider = "gemini"
model = "gemini-pro"
`
	tests := []struct {
		name     string
		policy   string
		env      map[string]string
		args     []string
		wantCode int
		contains []string
		absent   []string
	}{
		{
			name:     "defaults without a policy file",
			args:     []string{"formal", "providers", "--task", "proof"},
			wantCode: 0,
			contains: []string{"provider: anthropic", "model:    claude-opus-5-5", "https://api.anthropic.com", "ANTHROPIC_API_KEY (not set)"},
		},
		{
			name:     "both tasks, key set, never printed",
			policy:   policyFile,
			env:      map[string]string{"LOCAL_LLM_KEY": "sk-VERYSECRET-123", "GEMINI_API_KEY": "gem-SECRET-456"},
			args:     []string{"formal", "providers"},
			wantCode: 0,
			contains: []string{"task: proof", "provider: openai", "LOCAL_LLM_KEY (set)", "task: spec", "provider: gemini", "GEMINI_API_KEY (set)"},
			absent:   []string{"VERYSECRET", "SECRET-456"},
		},
		{
			name:     "flags override the file",
			policy:   policyFile,
			args:     []string{"formal", "providers", "--task", "spec", "--provider", "anthropic", "--model", "claude-sonnet-5-5"},
			wantCode: 0,
			contains: []string{"provider: anthropic", "claude-sonnet-5-5", "ANTHROPIC_API_KEY"},
			absent:   []string{"gemini"},
		},
		{
			name:     "unknown task",
			args:     []string{"formal", "providers", "--task", "nope"},
			wantCode: 3,
			contains: []string{"unknown task"},
		},
		{
			name:     "secret in policy is a config error",
			policy:   "[formal.llm]\napi_key = \"sk-x\"\n",
			args:     []string{"formal", "providers"},
			wantCode: 3,
			contains: []string{"looks like a secret"},
			absent:   []string{"sk-x"},
		},
		{
			name:     "missing model for openai",
			args:     []string{"formal", "providers", "--provider", "openai"},
			wantCode: 3,
			contains: []string{"no model is set for provider openai"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{"README.md": "x\n"}
			if tt.policy != "" {
				files[".paranoid.toml"] = tt.policy
			}
			formalTestRepo(t, files)
			for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "LOCAL_LLM_KEY"} {
				t.Setenv(k, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			out, code := runFormal(t, tt.args...)
			require.Equal(t, tt.wantCode, code, out)
			for _, c := range tt.contains {
				require.Contains(t, out, c)
			}
			for _, a := range tt.absent {
				require.NotContains(t, out, a)
			}
		})
	}
}
