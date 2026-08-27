package fixprompt

// This file checks that RenderJSON output validates against the
// checked-in JSON Schema at docs/schema/fix-prompt-v1.schema.json. It
// is a small validator scoped to the handful of keywords that schema
// uses, the same approach internal/report/schema_test.go takes for the
// verify report schema (the dependency allowlist has no JSON Schema
// library). It fails loudly on any keyword it does not know, so a
// schema edit can never silently skip validation.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func loadFixPromptSchema(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "schema", "fix-prompt-v1.schema.json"))
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	return schema
}

// validate checks one decoded JSON value against a schema node and
// returns every violation. root is the whole schema, for $ref lookups.
func validate(root, node map[string]any, value any, path string) []string {
	var errs []string
	for keyword, constraint := range node {
		switch keyword {
		case "$schema", "$id", "title", "$defs":
			// Annotation-only keywords carry no constraint.
		case "$ref":
			target, err := resolveFixPromptRef(root, constraint.(string))
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			errs = append(errs, validate(root, target, value, path)...)
		case "type":
			if !hasFixPromptType(value, constraint.(string)) {
				errs = append(errs, fmt.Sprintf("%s: want type %s, got %T", path, constraint, value))
			}
		case "const":
			if fmt.Sprintf("%v", value) != fmt.Sprintf("%v", constraint) {
				errs = append(errs, fmt.Sprintf("%s: want const %v, got %v", path, constraint, value))
			}
		case "enum":
			if !inFixPromptEnum(value, constraint.([]any)) {
				errs = append(errs, fmt.Sprintf("%s: %v not in enum %v", path, value, constraint))
			}
		case "minimum":
			if n, ok := value.(float64); ok && n < constraint.(float64) {
				errs = append(errs, fmt.Sprintf("%s: %v below minimum %v", path, n, constraint))
			}
		case "pattern":
			if s, ok := value.(string); ok {
				if !regexp.MustCompile(constraint.(string)).MatchString(s) {
					errs = append(errs, fmt.Sprintf("%s: %q does not match %q", path, s, constraint))
				}
			}
		case "required":
			obj, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for _, name := range constraint.([]any) {
				if _, present := obj[name.(string)]; !present {
					errs = append(errs, fmt.Sprintf("%s: missing required property %q", path, name))
				}
			}
		case "properties":
			obj, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for name, sub := range constraint.(map[string]any) {
				if child, present := obj[name]; present {
					errs = append(errs, validate(root, sub.(map[string]any), child, path+"/"+name)...)
				}
			}
		case "additionalProperties":
			obj, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if constraint != false {
				errs = append(errs, fmt.Sprintf("%s: only additionalProperties false is supported", path))
				continue
			}
			declared, _ := node["properties"].(map[string]any)
			for name := range obj {
				if _, known := declared[name]; !known {
					errs = append(errs, fmt.Sprintf("%s: unexpected property %q", path, name))
				}
			}
		case "items":
			list, ok := value.([]any)
			if !ok {
				continue
			}
			for i, item := range list {
				errs = append(errs, validate(root, constraint.(map[string]any), item, fmt.Sprintf("%s/%d", path, i))...)
			}
		default:
			errs = append(errs, fmt.Sprintf("%s: schema keyword %q is not supported by this validator", path, keyword))
		}
	}
	return errs
}

func resolveFixPromptRef(root map[string]any, ref string) (map[string]any, error) {
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return nil, fmt.Errorf("unsupported $ref %q", ref)
	}
	defs, ok := root["$defs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("$ref %q but the schema has no $defs", ref)
	}
	target, ok := defs[strings.TrimPrefix(ref, prefix)].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("$ref %q not found", ref)
	}
	return target, nil
}

func hasFixPromptType(value any, name string) bool {
	switch name {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		n, ok := value.(float64)
		return ok && n == math.Trunc(n)
	default:
		return false
	}
}

func inFixPromptEnum(value any, allowed []any) bool {
	for _, candidate := range allowed {
		if fmt.Sprintf("%v", value) == fmt.Sprintf("%v", candidate) {
			return true
		}
	}
	return false
}

func validateFixPromptReport(t *testing.T, rendered string) []string {
	t.Helper()
	var doc any
	require.NoError(t, json.Unmarshal([]byte(rendered), &doc))
	schema := loadFixPromptSchema(t)
	return validate(schema, schema, doc, "#")
}

func TestFixPromptJSONMatchesSchema(t *testing.T) {
	out, err := RenderJSON(samplePrompt())
	require.NoError(t, err)
	require.Empty(t, validateFixPromptReport(t, out))
}

func TestFixPromptJSONMatchesSchemaEmpty(t *testing.T) {
	out, err := RenderJSON(Prompt{SchemaVersion: SchemaVersion})
	require.NoError(t, err)
	require.Empty(t, validateFixPromptReport(t, out))
}

// TestFixPromptSchemaValidatorCatchesViolations proves the validator is
// alive: output broken in a few ways must fail validation.
func TestFixPromptSchemaValidatorCatchesViolations(t *testing.T) {
	out, err := RenderJSON(samplePrompt())
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		mutate  func(doc map[string]any)
		message string
	}{
		{
			name:    "bad schema version",
			mutate:  func(doc map[string]any) { doc["schema_version"] = float64(2) },
			message: "want const 1",
		},
		{
			name:    "extra top-level field",
			mutate:  func(doc map[string]any) { doc["extra"] = true },
			message: "unexpected property",
		},
		{
			name: "bad severity",
			mutate: func(doc map[string]any) {
				first := doc["instructions"].([]any)[0].(map[string]any)
				first["severity"] = "extreme"
			},
			message: "not in enum",
		},
		{
			name: "bad rule id",
			mutate: func(doc map[string]any) {
				first := doc["instructions"].([]any)[0].(map[string]any)
				first["rule_id"] = "nope"
			},
			message: "does not match",
		},
		{
			name: "missing required field",
			mutate: func(doc map[string]any) {
				first := doc["instructions"].([]any)[0].(map[string]any)
				delete(first, "line")
			},
			message: "missing required property",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(out), &doc))
			tc.mutate(doc)
			schema := loadFixPromptSchema(t)
			errs := validate(schema, schema, doc, "#")
			require.NotEmpty(t, errs)
			require.Contains(t, strings.Join(errs, "\n"), tc.message)
		})
	}
}
