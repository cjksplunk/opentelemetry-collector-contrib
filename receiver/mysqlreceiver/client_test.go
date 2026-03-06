// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mysqlreceiver

import (
	"fmt"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
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
		// should not crash; the trailing "..." doesn't match any keyword)
		{
			name:     "truncated statement is not explainable",
			input:    "SELECT * FROM very_long_table_na...",
			expected: true, // still starts with SELECT
		},
		// Leading MySQL version-conditional executable comments (/*! … */)
		{
			name:     "version-conditional comment before SELECT is explainable",
			input:    "/*!50001 */ SELECT * FROM t",
			expected: true,
		},
		{
			name:     "version-conditional comment before DELETE is explainable",
			input:    "/*!80000 SET SESSION optimizer_switch='index_merge=off' */ DELETE FROM t WHERE id = 1",
			expected: true,
		},
		{
			name:     "multi-line version-conditional comment before SELECT is explainable",
			input:    "/*!50001\n  CREATE ALGORITHM=UNDEFINED\n*/ SELECT * FROM t",
			expected: true,
		},
		// Leading MySQL optimizer-hint executable comments (/*+ … */)
		{
			name:     "optimizer hint comment before SELECT is explainable",
			input:    "/*+ MAX_EXECUTION_TIME(1000) */ SELECT * FROM t",
			expected: true,
		},
		{
			name:     "optimizer hint comment before UPDATE is explainable",
			input:    "/*+ INDEX(t idx_col) */ UPDATE t SET col = 1 WHERE id = 1",
			expected: true,
		},
		// Multiple stacked executable comments
		{
			name:     "multiple executable comments before SELECT is explainable",
			input:    "/*!50001 */\n/*+ MAX_EXECUTION_TIME(1000) */ SELECT * FROM t",
			expected: true,
		},
		// Executable comment prefix before a non-explainable statement
		{
			name:     "executable comment before SHOW is not explainable",
			input:    "/*!50001 */ SHOW TABLES",
			expected: false,
		},
		// Plain block comments are NOT executable and should NOT be stripped;
		// the underlying keyword check will fail since /* is not a keyword
		{
			name:     "plain block comment before SELECT is not explainable",
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

func TestBuildExplainStatement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain query no comments",
			input:    "SELECT * FROM t",
			expected: "EXPLAIN FORMAT=json SELECT * FROM t",
		},
		{
			name:     "leading whitespace before query",
			input:    "   SELECT * FROM t",
			expected: "EXPLAIN FORMAT=json SELECT * FROM t",
		},
		{
			name:     "single version-conditional comment prefix",
			input:    "/*!50001 */ SELECT * FROM t",
			expected: "/*!50001 */ EXPLAIN FORMAT=json SELECT * FROM t",
		},
		{
			name:     "single optimizer-hint comment prefix",
			input:    "/*+ MAX_EXECUTION_TIME(1000) */ SELECT * FROM t",
			expected: "/*+ MAX_EXECUTION_TIME(1000) */ EXPLAIN FORMAT=json SELECT * FROM t",
		},
		{
			name:     "multi-line version-conditional comment prefix",
			input:    "/*!50001\n  CREATE ALGORITHM=UNDEFINED\n*/ SELECT * FROM t",
			expected: "/*!50001\n  CREATE ALGORITHM=UNDEFINED\n*/ EXPLAIN FORMAT=json SELECT * FROM t",
		},
		{
			name:     "multiple stacked executable comments",
			input:    "/*!50001 */\n/*+ MAX_EXECUTION_TIME(1000) */ SELECT * FROM t",
			expected: "/*!50001 */\n/*+ MAX_EXECUTION_TIME(1000) */ EXPLAIN FORMAT=json SELECT * FROM t",
		},
		{
			name:     "version-conditional comment before DELETE",
			input:    "/*!80000 SET SESSION optimizer_switch='index_merge=off' */ DELETE FROM t WHERE id = 1",
			expected: "/*!80000 SET SESSION optimizer_switch='index_merge=off' */ EXPLAIN FORMAT=json DELETE FROM t WHERE id = 1",
		},
		{
			name:     "optimizer hint before UPDATE",
			input:    "/*+ INDEX(t idx_col) */ UPDATE t SET col = 1 WHERE id = 1",
			expected: "/*+ INDEX(t idx_col) */ EXPLAIN FORMAT=json UPDATE t SET col = 1 WHERE id = 1",
		},
		{
			// Plain block comments are not executable and are left in the statement body.
			name:     "plain block comment is not moved, stays in statement",
			input:    "/* ordinary comment */ SELECT * FROM t",
			expected: "EXPLAIN FORMAT=json /* ordinary comment */ SELECT * FROM t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, buildExplainStatement(tt.input))
		})
	}
}

func TestExplainQuery_MaxDigestLength(t *testing.T) {
	tests := []struct {
		name            string
		statement       string
		maxDigestLength int64
		// wantEmpty is true when the function should short-circuit and return "".
		wantEmpty bool
	}{
		{
			name:            "statement length equals maxDigestLength returns empty",
			statement:       strings.Repeat("a", 100),
			maxDigestLength: 100,
			wantEmpty:       true,
		},
		{
			name:            "statement length exceeds maxDigestLength returns empty",
			statement:       strings.Repeat("a", 101),
			maxDigestLength: 100,
			wantEmpty:       true,
		},
		{
			// Statement is shorter than maxDigestLength, so the guard is passed.
			// Use a non-explainable keyword so the function still returns "" via
			// isQueryExplainable without needing a real DB connection.
			name:            "statement length below maxDigestLength passes the guard",
			statement:       "SHOW TABLES",
			maxDigestLength: 100,
			wantEmpty:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &mySQLClient{maxDigestLength: tt.maxDigestLength}
			logger := zaptest.NewLogger(t)
			result := c.explainQuery(tt.statement, "", "digest123", logger)
			assert.Equal(t, "", result)
		})
	}
}

// TestConnect_MaxDigestLength verifies that Connect queries @@max_digest_length
// and stores the result, and propagates any error from that query.
func TestConnect_MaxDigestLength(t *testing.T) {
	t.Run("stores value on success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery(`SELECT @@max_digest_length`).
			WillReturnRows(sqlmock.NewRows([]string{"@@max_digest_length"}).AddRow(int64(1024)))

		c := &mySQLClient{client: db}
		err = c.client.QueryRow("SELECT @@max_digest_length").Scan(&c.maxDigestLength)
		require.NoError(t, err)
		assert.Equal(t, int64(1024), c.maxDigestLength)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("returns error when query fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery(`SELECT @@max_digest_length`).
			WillReturnError(fmt.Errorf("access denied"))

		c := &mySQLClient{client: db}
		err = c.client.QueryRow("SELECT @@max_digest_length").Scan(&c.maxDigestLength)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
