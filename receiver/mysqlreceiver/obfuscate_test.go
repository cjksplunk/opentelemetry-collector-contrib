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

func TestObfuscateSQLError(t *testing.T) {
	// An unterminated string literal causes ObfuscateSQLStringWithOptions to return an error.
	_, err := newObfuscator().obfuscateSQLString("SELECT 'unterminated")
	assert.Error(t, err)
}

func TestObfuscatePlanError(t *testing.T) {
	// Malformed JSON causes ObfuscateSQLExecPlan to return an error.
	_, err := newObfuscator().obfuscatePlan("{invalid json")
	assert.Error(t, err)
}

func TestNormalizePlanError(t *testing.T) {
	// Malformed JSON causes ObfuscateSQLExecPlan (normalize=true) to return an error.
	_, err := newObfuscator().normalizePlan("{invalid json")
	assert.Error(t, err)
}

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

// TestNormalizePlanSettingsFieldBehavior tests every individual key in defaultSQLPlanNormalizeSettings
// to document whether its value is kept verbatim (KeepValues) or replaced with "?" (ObfuscateSQLValues).
//
// No equivalent test exists for defaultSQLPlanObfuscateSettings because that config keeps almost all
// structural fields, so the full real-plan fixtures used in TestObfuscatePlan already exercise every
// entry in both its KeepValues and ObfuscateSQLValues lists without needing field-level cases.
func TestNormalizePlanSettingsFieldBehavior(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// --- KeepValues: value must be preserved verbatim ---
		{
			name:     "select_id is kept",
			input:    `{"query_block":{"select_id":42}}`,
			expected: `{"query_block":{"select_id":42}}`,
		},
		{
			name:     "using_filesort is kept",
			input:    `{"query_block":{"ordering_operation":{"using_filesort":false}}}`,
			expected: `{"query_block":{"ordering_operation":{"using_filesort":false}}}`,
		},
		{
			name:     "table_name is kept",
			input:    `{"query_block":{"table":{"table_name":"users"}}}`,
			expected: `{"query_block":{"table":{"table_name":"users"}}}`,
		},
		{
			name:     "access_type is kept",
			input:    `{"query_block":{"table":{"access_type":"ref"}}}`,
			expected: `{"query_block":{"table":{"access_type":"ref"}}}`,
		},
		{
			name:     "filtered is kept",
			input:    `{"query_block":{"table":{"filtered":"50.00"}}}`,
			expected: `{"query_block":{"table":{"filtered":"50.00"}}}`,
		},
		{
			name:     "used_columns is kept",
			input:    `{"query_block":{"table":{"used_columns":["id","name"]}}}`,
			expected: `{"query_block":{"table":{"used_columns":["id","name"]}}}`,
		},
		{
			// rows_examined_per_join is in KeepValues but NOT in ObfuscateSQLValues, so it is
			// always preserved — even when nested inside a "table" block.
			name:     "rows_examined_per_join is kept",
			input:    `{"query_block":{"table":{"rows_examined_per_join":500}}}`,
			expected: `{"query_block":{"table":{"rows_examined_per_join":500}}}`,
		},

		// --- ObfuscateSQLValues: value must be replaced with "?" ---
		{
			name:     "query_cost is obfuscated",
			input:    `{"query_block":{"cost_info":{"query_cost":"999.99"}}}`,
			expected: `{"query_block":{"cost_info":{"query_cost":"?"}}}`,
		},
		{
			// rows_examined_per_scan is in both KeepValues and ObfuscateSQLValues.
			// When nested inside "table" (itself a KeepValue), the keeping state is already
			// active, so the ObfuscateSQLValues transformer fires and replaces the value.
			name:     "rows_examined_per_scan is obfuscated when nested in table",
			input:    `{"query_block":{"table":{"rows_examined_per_scan":10000}}}`,
			expected: `{"query_block":{"table":{"rows_examined_per_scan":"?"}}}`,
		},
		{
			name:     "rows_produced_per_join is obfuscated when nested in table",
			input:    `{"query_block":{"table":{"rows_produced_per_join":5000}}}`,
			expected: `{"query_block":{"table":{"rows_produced_per_join":"?"}}}`,
		},
		{
			name:     "attached_condition is SQL-obfuscated",
			input:    `{"query_block":{"table":{"attached_condition":"x > 5 AND y = 'secret'"}}}`,
			expected: `{"query_block":{"table":{"attached_condition":"x > ? AND y = ?"}}}`,
		},
		{
			name:     "condition is SQL-obfuscated",
			input:    `{"inputs":[{"condition":"x > 5 AND y = 'secret'"}]}`,
			expected: `{"inputs":[{"condition":"x > ? AND y = ?"}]}`,
		},
		{
			name:     "operation is SQL-obfuscated",
			input:    `{"inputs":[{"operation":"Table scan on users WHERE id = 42"}]}`,
			expected: `{"inputs":[{"operation":"Table scan on users WHERE id = ?"}]}`,
		},
		{
			name:     "estimated_total_cost is obfuscated",
			input:    `{"inputs":[{"estimated_total_cost":459.47}]}`,
			expected: `{"inputs":[{"estimated_total_cost":"?"}]}`,
		},
		{
			name:     "estimated_rows is obfuscated",
			input:    `{"inputs":[{"estimated_rows":10000.0}]}`,
			expected: `{"inputs":[{"estimated_rows":"?"}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := newObfuscator().normalizePlan(tc.input)
			require.NoError(t, err)

			var resultJSON, expectedJSON any
			require.NoError(t, json.Unmarshal([]byte(result), &resultJSON))
			require.NoError(t, json.Unmarshal([]byte(tc.expected), &expectedJSON))

			assert.Equal(t, expectedJSON, resultJSON)
		})
	}
}

// runPlanTests is a helper that drives table-driven tests for plan obfuscation/normalization.
// planFunc is the method under test (e.g. obfuscator.obfuscatePlan or obfuscator.normalizePlan).
func runPlanTests(t *testing.T, planFunc func(string) (string, error), tests []struct {
	name         string
	inputFile    string
	expectedFile string
},
) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join("testdata", "obfuscate", tc.inputFile))
			require.NoError(t, err)

			result, err := planFunc(string(input))
			require.NoError(t, err)

			expected, err := os.ReadFile(filepath.Join("testdata", "obfuscate", tc.expectedFile))
			require.NoError(t, err)

			// Normalize JSON for comparison to ignore formatting differences
			var resultJSON, expectedJSON any
			require.NoError(t, json.Unmarshal([]byte(result), &resultJSON))
			require.NoError(t, json.Unmarshal(expected, &expectedJSON))

			assert.Equal(t, expectedJSON, resultJSON)
		})
	}
}

func TestNormalizePlan(t *testing.T) {
	// When normalize=true, ObfuscateSQLExecPlan uses defaultSQLPlanNormalizeSettings, which additionally
	// replaces numeric and structural values (estimated_rows, estimated_total_cost, access_type, table_name,
	// select_id, using_filesort, etc.) with "?" compared to the obfuscate-only path.
	runPlanTests(t, newObfuscator().normalizePlan, []struct {
		name         string
		inputFile    string
		expectedFile string
	}{
		{
			name:         "version1_query_block_normalized",
			inputFile:    "inputQueryPlan.json",
			expectedFile: "expectedQueryPlanNormalized.json",
		},
		{
			name:         "version2_inputs_array_normalized",
			inputFile:    "inputQueryPlanV2.json",
			expectedFile: "expectedQueryPlanV2Normalized.json",
		},
	})
}

func TestObfuscatePlan(t *testing.T) {
	// MySQL 8.4 EXPLAIN FORMAT=JSON produces two formats:
	//   Version 1 (default, explain_json_format_version=1): query_block → ordering_operation → table → attached_condition
	//   Version 2 (explain_json_format_version=2):          query + inputs array, each node has condition/operation/access_type
	runPlanTests(t, newObfuscator().obfuscatePlan, []struct {
		name         string
		inputFile    string
		expectedFile string
	}{
		{
			name:         "version1_query_block",
			inputFile:    "inputQueryPlan.json",
			expectedFile: "expectedQueryPlan.json",
		},
		{
			name:         "version2_inputs_array",
			inputFile:    "inputQueryPlanV2.json",
			expectedFile: "expectedQueryPlanV2.json",
		},
	})
}
