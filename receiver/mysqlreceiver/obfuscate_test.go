// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mysqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mysqlreceiver"

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObfuscateSQL(t *testing.T) {
	expected, err := os.ReadFile(filepath.Join("testdata", "obfuscate", "expectedSQL.sql"))
	assert.NoError(t, err)
	expectedSQL := strings.TrimSpace(string(expected))

	input, err := os.ReadFile(filepath.Join("testdata", "obfuscate", "inputSQL.sql"))
	assert.NoError(t, err)

	result, err := newObfuscator().obfuscateSQLString(string(input))
	assert.NoError(t, err)
	assert.Equal(t, expectedSQL, result)
}

func TestObfuscatePlan(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "obfuscate", "inputQueryPlan.json"))
	require.NoError(t, err)

	result, err := newObfuscator().obfuscatePlan(string(input))
	require.NoError(t, err)

	// Parse both input and output as JSON
	var inputPlan, resultPlan map[string]any
	err = json.Unmarshal(input, &inputPlan)
	require.NoError(t, err)

	err = json.Unmarshal([]byte(result), &resultPlan)
	require.NoError(t, err)

	// Validate that plan structure and non-obfuscatable values are preserved
	validatePlanStructurePreserved(t, inputPlan, resultPlan)

	// Validate that obfuscatable fields (like attached_condition) are actually obfuscated
	inputAttached := inputPlan["query"]
	resultAttached := resultPlan["query"]

	// The attached_condition should be obfuscated (values replaced with ?)
	require.NotEqual(t, inputAttached, resultAttached, "query should be obfuscated")
	assert.NotContains(t, resultAttached, "'EXPLAIN %'", "obfuscated plan should not contain literal string values")
	assert.NotContains(t, resultAttached, "'/* otel-collector-ignore */%'", "obfuscated plan should not contain literal string values")
}

// validatePlanStructurePreserved validates that all plan structure and non-obfuscatable values are preserved
func validatePlanStructurePreserved(t *testing.T, input, result map[string]any) {
	for key, inputValue := range input {
		resultValue, exists := result[key]
		require.True(t, exists, "key %s missing from obfuscated plan", key)

		switch v := inputValue.(type) {
		case map[string]any:
			resultMap, ok := resultValue.(map[string]any)
			require.True(t, ok, "value for key %s should be a map", key)
			validatePlanStructurePreserved(t, v, resultMap)

		case []any:
			resultSlice, ok := resultValue.([]any)
			require.True(t, ok, "value for key %s should be a slice", key)
			assert.Len(t, resultSlice, len(v), "slice length mismatch for key %s", key)
			for i, item := range v {
				if itemMap, ok := item.(map[string]any); ok {
					resultItemMap, ok := resultSlice[i].(map[string]any)
					require.True(t, ok, "slice item at index %d for key %s should be a map", i, key)
					validatePlanStructurePreserved(t, itemMap, resultItemMap)
				} else {
					// Verify non-map items are preserved
					assert.Equal(t, item, resultSlice[i], "slice item at index %d for key %s value was modified", i, key)
				}
			}

		case string, float64, bool, nil:
			// Preserve non-obfuscatable string values (like field names, numbers, booleans)
			switch key {
			case "query", "operation", "condition":
				// we expect these to be obfuscated if the obfuscator detects the need to do so, otherwise the value will remain unchanged
				continue

			default:
				assert.Equal(t, inputValue, resultValue, "value for field %s was modified", key)
			}
		}
	}
}
