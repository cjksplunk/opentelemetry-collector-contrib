// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mysqlreceiver

import (
	"testing"

	version "github.com/hashicorp/go-version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestIsQueryExplainable(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Supported keywords — plain queries
		{
			name:     "select is explainable",
			input:    "SELECT * FROM t",
			expected: true,
		},
		{
			name:     "delete is explainable",
			input:    "DELETE FROM t WHERE id = 1",
			expected: true,
		},
		{
			name:     "insert is explainable",
			input:    "INSERT INTO t VALUES (1)",
			expected: true,
		},
		{
			name:     "replace is explainable",
			input:    "REPLACE INTO t VALUES (1)",
			expected: true,
		},
		{
			name:     "update is explainable",
			input:    "UPDATE t SET col = 1",
			expected: true,
		},
		// Case-insensitive matching
		{
			name:     "mixed-case SELECT is explainable",
			input:    "Select * FROM t",
			expected: true,
		},
		// Leading whitespace
		{
			name:     "leading whitespace before SELECT is explainable",
			input:    "   SELECT * FROM t",
			expected: true,
		},
		// Unsupported statements
		{
			name:     "show is not explainable",
			input:    "SHOW TABLES",
			expected: false,
		},
		{
			name:     "create is not explainable",
			input:    "CREATE TABLE t (id INT)",
			expected: false,
		},
		{
			name:     "drop is not explainable",
			input:    "DROP TABLE t",
			expected: false,
		},
		{
			name:     "empty string is not explainable",
			input:    "",
			expected: false,
		},
		// Truncated statements (handled upstream, but isQueryExplainable itself
		// should not crash; the trailing "..." doesn’t match any keyword)
		{
			name:     "truncated statement that starts with SELECT is still type-explainable",
			input:    "SELECT * FROM very_long_table_na...",
			expected: true, // still starts with SELECT
		},
		// Any leading block comment makes the query not explainable; digest_text
		// from performance_schema does not include them, so this won't arise in practice.
		{
			name:     "leading block comment before SELECT is not explainable",
			input:    "/* a comment */ SELECT * FROM t",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isQueryExplainable(tt.input))
		})
	}
}

// TestExplainQueryEarlyExits verifies that explainQuery returns "" without
// hitting the database when the sample statement is truncated or the digest
// text is not an explainable statement type.
func TestExplainQueryEarlyExits(t *testing.T) {
	// mySQLClient with a nil DB — safe because both early-exit paths return
	// before any database call is made.
	c := &mySQLClient{}
	logger := zap.NewNop()

	t.Run("truncated sample statement returns empty", func(t *testing.T) {
		result := c.explainQuery("SELECT * FROM t", "SELECT * FROM very_long_table_na...", "", "digest1", logger)
		assert.Empty(t, result)
	})

	t.Run("non-explainable digest text returns empty", func(t *testing.T) {
		result := c.explainQuery("SHOW TABLES", "SHOW TABLES", "", "digest2", logger)
		assert.Empty(t, result)
	})
}

// mustParseVersion is a test helper that parses a semver string and fails the test if parsing fails.
func mustParseVersion(t *testing.T, v string) *version.Version {
	t.Helper()
	parsed, err := version.NewVersion(v)
	require.NoError(t, err)
	return parsed
}

// TestDBVersionCapabilities tests the capability predicates on the dbVersion struct
// for MySQL 8+, MySQL 5.7, MariaDB 10.x, and MariaDB 11.x.
func TestDBVersionCapabilities(t *testing.T) {
	tests := []struct {
		name                        string
		dv                          dbVersion
		wantIsMySQL8Plus            bool
		wantSupportsQuerySampleText bool
	}{
		{
			name:                        "MySQL 8.0.27",
			dv:                          dbVersion{product: dbProductMySQL, version: mustParseVersion(t, "8.0.27")},
			wantIsMySQL8Plus:            true,
			wantSupportsQuerySampleText: true,
		},
		{
			name:                        "MySQL 8.0.22 (minimum for query_sample_text)",
			dv:                          dbVersion{product: dbProductMySQL, version: mustParseVersion(t, "8.0.22")},
			wantIsMySQL8Plus:            true,
			wantSupportsQuerySampleText: true,
		},
		{
			name:                        "MySQL 5.7.44",
			dv:                          dbVersion{product: dbProductMySQL, version: mustParseVersion(t, "5.7.44")},
			wantIsMySQL8Plus:            false,
			wantSupportsQuerySampleText: false,
		},
		{
			name:                        "MySQL 5.6.51",
			dv:                          dbVersion{product: dbProductMySQL, version: mustParseVersion(t, "5.6.51")},
			wantIsMySQL8Plus:            false,
			wantSupportsQuerySampleText: false,
		},
		{
			name:                        "MariaDB 10.11.6",
			dv:                          dbVersion{product: dbProductMariaDB, version: mustParseVersion(t, "10.11.6")},
			wantIsMySQL8Plus:            false,
			wantSupportsQuerySampleText: false,
		},
		{
			name:                        "MariaDB 11.4.2",
			dv:                          dbVersion{product: dbProductMariaDB, version: mustParseVersion(t, "11.4.2")},
			wantIsMySQL8Plus:            false,
			wantSupportsQuerySampleText: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantIsMySQL8Plus, tt.dv.isMySQL8Plus(), "isMySQL8Plus()")
			assert.Equal(t, tt.wantSupportsQuerySampleText, tt.dv.supportsQuerySampleText(), "supportsQuerySampleText()")
		})
	}
}

// TestGetDBVersionCaching verifies that a cached version is returned on subsequent
// calls and that no additional query is made.
func TestGetDBVersionCaching(t *testing.T) {
	// Pre-populate the cache on mySQLClient directly.
	preloaded := dbVersion{product: dbProductMySQL, version: mustParseVersion(t, "8.0.27")}
	c := &mySQLClient{
		cachedDBVersion: &preloaded,
		// client field is nil — any real DB call would panic, proving the cache is hit.
	}

	got, err := c.getDBVersion()
	require.NoError(t, err)
	assert.Equal(t, preloaded.product, got.product)
	assert.Equal(t, preloaded.version.String(), got.version.String())
}
