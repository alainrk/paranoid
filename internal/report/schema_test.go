package report

// This file checks that RenderJSON output validates against the
// checked-in JSON Schema at docs/schema/report-v1.schema.json. The
// dependency allowlist has no JSON Schema library, so a small validator
// lives here. It supports only the keywords the schema uses and fails
// loudly on any keyword it does not know, so a schema edit cannot
// silently skip validation.

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

	"github.com/alainrk/paranoid/internal/finding"
)

// schemaKeywords the validator understands. Annotation-only keywords
// carry no constraint and are listed so they do not trip the unknown
// keyword check.
var schemaAnnotations = map[string]bool{
	"$schema": true,
	"$id":     true,
	"title":   true,
	"$defs":   true,
}

// loadSchema reads and decodes the checked-in schema file.
func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "schema", "report-v1.schema.json"))
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
		case "$ref":
			target, err := resolveRef(root, constraint.(string))
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			errs = append(errs, validate(root, target, value, path)...)
		case "type":
			if !hasType(value, constraint.(string)) {
				errs = append(errs, fmt.Sprintf("%s: want type %s, got %T", path, constraint, value))
			}
		case "const":
			if !equalJSON(value, constraint) {
				errs = append(errs, fmt.Sprintf("%s: want const %v, got %v", path, constraint, value))
			}
		case "enum":
			if !inEnum(value, constraint.([]any)) {
				errs = append(errs, fmt.Sprintf("%s: %v not in enum %v", path, value, constraint))
			}
		case "minimum":
			if n, ok := value.(float64); ok && n < constraint.(float64) {
				errs = append(errs, fmt.Sprintf("%s: %v below minimum %v", path, n, constraint))
			}
		case "maximum":
			if n, ok := value.(float64); ok && n > constraint.(float64) {
				errs = append(errs, fmt.Sprintf("%s: %v above maximum %v", path, n, constraint))
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
			if !schemaAnnotations[keyword] {
				errs = append(errs, fmt.Sprintf("%s: schema keyword %q is not supported by this validator", path, keyword))
			}
		}
	}
	return errs
}

// resolveRef follows a local "#/$defs/name" reference.
func resolveRef(root map[string]any, ref string) (map[string]any, error) {
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

// hasType checks a decoded JSON value against a schema type name.
func hasType(value any, name string) bool {
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
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

// equalJSON compares two decoded JSON scalars.
func equalJSON(a, b any) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// inEnum reports whether a value is one of the allowed enum values.
func inEnum(value any, allowed []any) bool {
	for _, candidate := range allowed {
		if equalJSON(value, candidate) {
			return true
		}
	}
	return false
}

// validateReport decodes rendered JSON and validates it.
func validateReport(t *testing.T, rendered string) []string {
	t.Helper()
	var doc any
	require.NoError(t, json.Unmarshal([]byte(rendered), &doc))
	schema := loadSchema(t)
	return validate(schema, schema, doc, "#")
}

func TestJSONOutputMatchesSchema(t *testing.T) {
	out, err := RenderJSON(sampleResult())
	require.NoError(t, err)
	require.Empty(t, validateReport(t, out))
}

func TestJSONOutputMatchesSchemaWithSkips(t *testing.T) {
	res := sampleResult()
	res.Findings = nil
	res.ChecksSkipped = []finding.SkippedCheck{{Name: "sandbox", Reason: "not implemented yet"}}
	out, err := RenderJSON(res)
	require.NoError(t, err)
	require.Empty(t, validateReport(t, out))
}

// TestSchemaValidatorCatchesViolations proves the validator is alive:
// a report broken in several ways must fail validation.
func TestSchemaValidatorCatchesViolations(t *testing.T) {
	out, err := RenderJSON(sampleResult())
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		mutate  func(doc map[string]any)
		message string
	}{
		{
			name:    "bad verdict",
			mutate:  func(doc map[string]any) { doc["verdict"] = "maybe" },
			message: "not in enum",
		},
		{
			name:    "score above 100",
			mutate:  func(doc map[string]any) { doc["score"] = float64(101) },
			message: "above maximum",
		},
		{
			name:    "fractional score",
			mutate:  func(doc map[string]any) { doc["score"] = 99.5 },
			message: "want type integer",
		},
		{
			name:    "wrong schema version",
			mutate:  func(doc map[string]any) { doc["schema_version"] = float64(8) },
			message: "want const 7",
		},
		{
			name:    "extra top-level field",
			mutate:  func(doc map[string]any) { doc["telemetry"] = true },
			message: "unexpected property",
		},
		{
			name:    "missing meta field",
			mutate:  func(doc map[string]any) { delete(doc["meta"].(map[string]any), "repo") },
			message: "missing required property",
		},
		{
			name: "bad rule id",
			mutate: func(doc map[string]any) {
				first := doc["findings"].([]any)[0].(map[string]any)
				first["rule_id"] = "nope"
			},
			message: "does not match",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(out), &doc))
			tc.mutate(doc)
			schema := loadSchema(t)
			errs := validate(schema, schema, doc, "#")
			require.NotEmpty(t, errs)
			require.Contains(t, strings.Join(errs, "\n"), tc.message)
		})
	}
}
