package report

// This file validates RenderSARIF output against a small, hand-rolled
// subset of the SARIF 2.1.0 object model: only the fields GitHub code
// scanning actually reads (tool.driver.name, rules[].id plus their
// short and full descriptions and helpUri, results[].ruleId,
// results[].level, results[].message.text, and
// results[].locations[].physicalLocation). This project's dependency
// allowlist has no JSON Schema library and the full SARIF schema is
// about 200KB; vendoring it for one format is out of proportion to
// what this tool needs to prove. The validator also rejects any
// top-level key on the log object other than the three paranoid
// actually emits ($schema, version, runs), so an accidental extra
// field cannot slip in unnoticed. It does not check the rest of the
// SARIF object model (invocations, artifacts, taxonomies, and so on);
// paranoid never emits those, so there is nothing to validate there.

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/rules"
)

// sarifTopLevelKeys are the only keys RenderSARIF ever writes on the
// log object.
var sarifTopLevelKeys = map[string]bool{"$schema": true, "version": true, "runs": true}

// sarifRunKeys are the only keys RenderSARIF ever writes on one run.
var sarifRunKeys = map[string]bool{"tool": true, "results": true, "properties": true}

// validateSARIFSubset checks a decoded SARIF log against the required
// subset described at the top of this file and returns every
// violation found.
func validateSARIFSubset(doc map[string]any) []string {
	var errs []string

	for key := range doc {
		if !sarifTopLevelKeys[key] {
			errs = append(errs, fmt.Sprintf("#: unexpected top-level key %q", key))
		}
	}
	if _, ok := doc["$schema"].(string); !ok {
		errs = append(errs, "#/$schema: want a string")
	}
	if v, ok := doc["version"].(string); !ok || v != "2.1.0" {
		errs = append(errs, "#/version: want the string 2.1.0")
	}

	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) != 1 {
		errs = append(errs, "#/runs: want exactly one run")
		return errs
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		errs = append(errs, "#/runs/0: want an object")
		return errs
	}
	for key := range run {
		if !sarifRunKeys[key] {
			errs = append(errs, fmt.Sprintf("#/runs/0: unexpected key %q", key))
		}
	}

	driver := digObject(run, "tool", "driver")
	if driver == nil {
		errs = append(errs, "#/runs/0/tool/driver: missing")
	} else {
		if name, ok := driver["name"].(string); !ok || name == "" {
			errs = append(errs, "#/runs/0/tool/driver/name: want a non-empty string")
		}
		ruleList, ok := driver["rules"].([]any)
		if !ok {
			errs = append(errs, "#/runs/0/tool/driver/rules: want an array")
		} else {
			for i, raw := range ruleList {
				path := fmt.Sprintf("#/runs/0/tool/driver/rules/%d", i)
				rule, ok := raw.(map[string]any)
				if !ok {
					errs = append(errs, path+": want an object")
					continue
				}
				if id, ok := rule["id"].(string); !ok || id == "" {
					errs = append(errs, path+"/id: want a non-empty string")
				}
				if digText(rule, "shortDescription") == "" {
					errs = append(errs, path+"/shortDescription/text: want a non-empty string")
				}
				if digText(rule, "fullDescription") == "" {
					errs = append(errs, path+"/fullDescription/text: want a non-empty string")
				}
				if uri, ok := rule["helpUri"].(string); !ok || uri == "" {
					errs = append(errs, path+"/helpUri: want a non-empty string")
				}
			}
		}
	}

	results, ok := run["results"].([]any)
	if !ok {
		errs = append(errs, "#/runs/0/results: want an array")
	} else {
		validLevels := map[string]bool{"error": true, "warning": true, "note": true, "none": true}
		for i, raw := range results {
			path := fmt.Sprintf("#/runs/0/results/%d", i)
			res, ok := raw.(map[string]any)
			if !ok {
				errs = append(errs, path+": want an object")
				continue
			}
			if id, ok := res["ruleId"].(string); !ok || id == "" {
				errs = append(errs, path+"/ruleId: want a non-empty string")
			}
			if level, ok := res["level"].(string); !ok || !validLevels[level] {
				errs = append(errs, path+"/level: want one of error, warning, note, none")
			}
			if digText(res, "message") == "" {
				errs = append(errs, path+"/message/text: want a non-empty string")
			}
			if locsRaw, present := res["locations"]; present {
				locs, ok := locsRaw.([]any)
				if !ok {
					errs = append(errs, path+"/locations: want an array")
					continue
				}
				for j, locRaw := range locs {
					locPath := fmt.Sprintf("%s/locations/%d", path, j)
					uri := digObject(toObject(locRaw), "physicalLocation", "artifactLocation")
					if uri == nil {
						errs = append(errs, locPath+"/physicalLocation/artifactLocation: missing")
						continue
					}
					if u, ok := uri["uri"].(string); !ok || u == "" {
						errs = append(errs, locPath+"/physicalLocation/artifactLocation/uri: want a non-empty string")
					}
				}
			}
		}
	}

	props := digObject(run, "properties")
	if props == nil {
		errs = append(errs, "#/runs/0/properties: missing")
	} else {
		if _, ok := props["score"].(float64); !ok {
			errs = append(errs, "#/runs/0/properties/score: want a number")
		}
		if _, ok := props["verdict"].(string); !ok {
			errs = append(errs, "#/runs/0/properties/verdict: want a string")
		}
		if _, ok := props["checks_run"].([]any); !ok {
			errs = append(errs, "#/runs/0/properties/checks_run: want an array")
		}
		if _, ok := props["checks_skipped"].([]any); !ok {
			errs = append(errs, "#/runs/0/properties/checks_skipped: want an array")
		}
		if _, ok := props["suppressed"].(map[string]any); !ok {
			errs = append(errs, "#/runs/0/properties/suppressed: want an object")
		}
	}

	return errs
}

// toObject type-asserts a decoded JSON value to an object, or nil.
func toObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// digObject walks a chain of object keys, returning nil if any step
// is missing or not an object.
func digObject(obj map[string]any, keys ...string) map[string]any {
	cur := obj
	for _, k := range keys {
		if cur == nil {
			return nil
		}
		cur = toObject(cur[k])
	}
	return cur
}

// digText reads the "text" field of a SARIF message-shaped object
// (shortDescription, fullDescription, message) nested under key.
func digText(obj map[string]any, key string) string {
	sub := toObject(obj[key])
	if sub == nil {
		return ""
	}
	s, _ := sub["text"].(string)
	return s
}

func decodeSARIF(t *testing.T, rendered string) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(rendered), &doc))
	return doc
}

func TestRenderSARIFMatchesRequiredSubset(t *testing.T) {
	out, err := RenderSARIF(sampleResult())
	require.NoError(t, err)
	require.Empty(t, validateSARIFSubset(decodeSARIF(t, out)))
}

// TestSARIFValidatorCatchesViolations proves the hand-rolled validator
// is alive: a report broken in several ways must fail validation.
func TestSARIFValidatorCatchesViolations(t *testing.T) {
	out, err := RenderSARIF(sampleResult())
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		mutate  func(doc map[string]any)
		message string
	}{
		{
			name:    "extra top-level key",
			mutate:  func(doc map[string]any) { doc["telemetry"] = true },
			message: "unexpected top-level key",
		},
		{
			name:    "wrong version",
			mutate:  func(doc map[string]any) { doc["version"] = "2.0.0" },
			message: "want the string 2.1.0",
		},
		{
			name: "bad result level",
			mutate: func(doc map[string]any) {
				run := doc["runs"].([]any)[0].(map[string]any)
				result := run["results"].([]any)[0].(map[string]any)
				result["level"] = "critical"
			},
			message: "want one of error, warning, note, none",
		},
		{
			name: "missing rule id",
			mutate: func(doc map[string]any) {
				run := doc["runs"].([]any)[0].(map[string]any)
				driver := run["tool"].(map[string]any)["driver"].(map[string]any)
				rule := driver["rules"].([]any)[0].(map[string]any)
				delete(rule, "id")
			},
			message: "want a non-empty string",
		},
		{
			name: "missing score",
			mutate: func(doc map[string]any) {
				run := doc["runs"].([]any)[0].(map[string]any)
				delete(run["properties"].(map[string]any), "score")
			},
			message: "score: want a number",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := decodeSARIF(t, out)
			tc.mutate(doc)
			errs := validateSARIFSubset(doc)
			require.NotEmpty(t, errs)
			joined := fmt.Sprintf("%v", errs)
			require.Contains(t, joined, tc.message)
		})
	}
}

func TestRenderSARIF(t *testing.T) {
	out, err := RenderSARIF(sampleResult())
	require.NoError(t, err)

	doc := decodeSARIF(t, out)
	require.Equal(t, "2.1.0", doc["version"])
	run := doc["runs"].([]any)[0].(map[string]any)
	driver := run["tool"].(map[string]any)["driver"].(map[string]any)
	require.Equal(t, "paranoid", driver["name"])
	require.Equal(t, "dev", driver["version"])

	rulesList := driver["rules"].([]any)
	require.Len(t, rulesList, len(rules.All()), "one reportingDescriptor per registered rule")

	results := run["results"].([]any)
	require.Len(t, results, 2)
	first := results[0].(map[string]any)
	require.Equal(t, "TI001", first["ruleId"])
	require.Equal(t, "error", first["level"], "critical maps to error")
	require.Equal(t, "A test file that existed at base is deleted at head.", digText(first, "message"))
	loc := first["locations"].([]any)[0].(map[string]any)
	artifact := digObject(loc, "physicalLocation", "artifactLocation")
	require.Equal(t, "calc_test.go", artifact["uri"])
	require.NotContains(t, toObject(loc["physicalLocation"]), "region", "no line means no region")
	props := first["properties"].(map[string]any)
	require.Equal(t, "critical", props["severity"], "original severity survives in properties")

	second := results[1].(map[string]any)
	require.Equal(t, "TI004", second["ruleId"])
	require.Equal(t, "error", second["level"], "high also maps to error")
	loc2 := second["locations"].([]any)[0].(map[string]any)
	region := digObject(loc2, "physicalLocation")["region"].(map[string]any)
	require.Equal(t, float64(12), region["startLine"])

	runProps := run["properties"].(map[string]any)
	require.Equal(t, float64(65), runProps["score"])
	require.Equal(t, "warn", runProps["verdict"])
	suppressed := runProps["suppressed"].(map[string]any)
	require.Equal(t, float64(1), suppressed["count"])
	entries := suppressed["entries"].([]any)
	require.Len(t, entries, 1)
	require.Equal(t, "TI006", entries[0].(map[string]any)["rule_id"])
}

func TestRenderSARIFNoLocationForFilelessFinding(t *testing.T) {
	res := sampleResult()
	res.Findings[0].File = ""
	res.Findings[0].Line = 0

	out, err := RenderSARIF(res)
	require.NoError(t, err)
	doc := decodeSARIF(t, out)
	run := doc["runs"].([]any)[0].(map[string]any)
	first := run["results"].([]any)[0].(map[string]any)
	require.NotContains(t, first, "locations")
}

func TestRenderSARIFIsDeterministic(t *testing.T) {
	first, err := RenderSARIF(sampleResult())
	require.NoError(t, err)
	second, err := RenderSARIF(sampleResult())
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestRenderSARIFHelpURIAnchorsIntoRulesDocs(t *testing.T) {
	out, err := RenderSARIF(sampleResult())
	require.NoError(t, err)
	doc := decodeSARIF(t, out)
	run := doc["runs"].([]any)[0].(map[string]any)
	driver := run["tool"].(map[string]any)["driver"].(map[string]any)
	for _, raw := range driver["rules"].([]any) {
		rule := raw.(map[string]any)
		if rule["id"] == "AS001" {
			require.Equal(t,
				sarifDocsBaseURL+"#as001-public-symbol-removed-while-claims-say-fix-or-feature",
				rule["helpUri"])
			return
		}
	}
	t.Fatal("AS001 not found in the rule list")
}
