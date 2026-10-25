package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLLMTables(t *testing.T) {
	in := `[formal.llm]
provider = "anthropic"
model = "claude-opus-5-5"

[formal.llm.proof]
provider = "openai"
model = "my-local-prover"
base_url = "http://localhost:11434/v1"
api_key_env = "LOCAL_LLM_KEY"

[formal.llm.spec]
provider = "gemini"
model = "gemini-pro"
`
	f, err := Parse([]byte(in))
	require.NoError(t, err)
	require.Equal(t, LLM{Provider: "anthropic", Model: "claude-opus-5-5"}, f.Formal.LLM)
	require.Equal(t, LLM{Provider: "openai", Model: "my-local-prover", BaseURL: "http://localhost:11434/v1", APIKeyEnv: "LOCAL_LLM_KEY"}, f.Formal.Proof)
	require.Equal(t, LLM{Provider: "gemini", Model: "gemini-pro"}, f.Formal.Spec)
}

func TestParseLLMInvalid(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{"api_key", "[formal.llm]\napi_key = \"sk-abc\"\n", "looks like a secret"},
		{"key", "[formal.llm.proof]\nkey = \"x\"\n", "looks like a secret"},
		{"token", "[formal.llm.spec]\ntoken = \"x\"\n", "looks like a secret"},
		{"secret", "[formal.llm]\nclient_secret = \"x\"\n", "looks like a secret"},
		{"suffix key", "[formal.llm]\nopenai_key = \"x\"\n", "looks like a secret"},
		{"unknown key", "[formal.llm]\ntemperature = \"1\"\n", `unknown key "temperature"`},
		{"unknown task", "[formal.llm.other]\n", "unknown table"},
		{"duplicate", "[formal.llm]\n[formal.llm]\n", "given more than once"},
		{"duplicate task", "[formal.llm.proof]\n[formal.llm.proof]\n", "given more than once"},
		{"bad provider", "[formal.llm]\nprovider = \"mystery\"\n", "want anthropic, openai, or gemini"},
		{"empty model", "[formal.llm]\nmodel = \"\"\n", "is empty"},
		{"not a string", "[formal.llm]\nmodel = 3\n", "quoted string"},
		{"bad url scheme", "[formal.llm]\nbase_url = \"ftp://x\"\n", "http or https"},
		{"url with password", "[formal.llm]\nbase_url = \"http://u:p@host/v1\"\n", "user name or password"},
		{"env name is a key", "[formal.llm]\napi_key_env = \"sk-ant-abc123\"\n", "NAME of an environment variable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseLLMErrorNeverEchoesTheValue(t *testing.T) {
	_, err := Parse([]byte("[formal.llm]\napi_key_env = \"sk-ant-SECRETVALUE\"\n"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRETVALUE")
	_, err = Parse([]byte("[formal.llm]\napi_key = \"sk-ant-SECRETVALUE\"\n"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRETVALUE")
}
