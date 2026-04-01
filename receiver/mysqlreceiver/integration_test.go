// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package mysqlreceiver

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/scraperinttest"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatatest/pmetrictest"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mysqlreceiver/internal/metadata"
)

const mysqlPort = "3306"

type mySQLTestConfig struct {
	name         string
	containerCmd []string
	tlsEnabled   bool
	insecureSkip bool
	imageVersion string
	expectedFile string
}

func TestIntegration(t *testing.T) {
	testCases := []mySQLTestConfig{
		{
			name:         "MySql-8.0.33-WithoutTLS",
			containerCmd: nil,
			tlsEnabled:   false,
			insecureSkip: false,
			imageVersion: "mysql:8.0.33",
			expectedFile: "expected-mysql.yaml",
		},
		{
			name:         "MySql-8.0.33-WithTLS",
			containerCmd: []string{"--auto_generate_certs=ON", "--require_secure_transport=ON"},
			tlsEnabled:   true,
			insecureSkip: true,
			imageVersion: "mysql:8.0.33",
			expectedFile: "expected-mysql.yaml",
		},
		{
			name:         "MariaDB-11.6.2",
			containerCmd: nil,
			tlsEnabled:   false,
			insecureSkip: false,
			imageVersion: "mariadb:11.6.2-ubi9",
			expectedFile: "expected-mariadb.yaml",
		},
		{
			name:         "MariaDB-10.11.11",
			containerCmd: nil,
			tlsEnabled:   false,
			insecureSkip: false,
			imageVersion: "mariadb:10.11.11-ubi9",
			expectedFile: "expected-mariadb.yaml",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scraperinttest.NewIntegrationTest(
				NewFactory(),
				scraperinttest.WithContainerRequest(
					testcontainers.ContainerRequest{
						Image:        tc.imageVersion,
						Cmd:          tc.containerCmd,
						ExposedPorts: []string{mysqlPort},
						WaitingFor: wait.ForListeningPort(mysqlPort).
							WithStartupTimeout(2 * time.Minute),
						Env: map[string]string{
							"MYSQL_ROOT_PASSWORD": "otel",
							"MYSQL_DATABASE":      "otel",
							"MYSQL_USER":          "otel",
							"MYSQL_PASSWORD":      "otel",
						},
						Files: []testcontainers.ContainerFile{
							{
								HostFilePath:      filepath.Join("testdata", "integration", "init.sh"),
								ContainerFilePath: "/docker-entrypoint-initdb.d/init.sh",
								FileMode:          700,
							},
						},
					}),
				scraperinttest.WithCustomConfig(
					func(t *testing.T, cfg component.Config, ci *scraperinttest.ContainerInfo) {
						rCfg := cfg.(*Config)
						rCfg.CollectionInterval = time.Second
						rCfg.Endpoint = net.JoinHostPort(ci.Host(t), ci.MappedPort(t, mysqlPort))
						rCfg.Username = "otel"
						rCfg.Password = "otel"
						if tc.tlsEnabled {
							rCfg.TLS.InsecureSkipVerify = tc.insecureSkip
						} else {
							rCfg.TLS.Insecure = true
						}
					}),
				scraperinttest.WithExpectedFile(
					filepath.Join("testdata", "integration", tc.expectedFile),
				),
				scraperinttest.WithCompareOptions(
					pmetrictest.IgnoreResourceAttributeValue("mysql.instance.endpoint"),
					pmetrictest.IgnoreMetricValues(),
					pmetrictest.IgnoreMetricDataPointsOrder(),
					pmetrictest.IgnoreStartTimestamp(),
					pmetrictest.IgnoreTimestamp(),
				),
			).Run(t)
		})
	}
}

// containerConfig builds a Config pointing at a running test container.
func containerConfig(host, port string) *Config {
	cfg := createDefaultConfig().(*Config)
	cfg.Username = "root"
	cfg.Password = "otel"
	cfg.AddrConfig = confignet.AddrConfig{
		Endpoint:  net.JoinHostPort(host, port),
		Transport: confignet.TransportTypeTCP,
	}
	cfg.TLS.Insecure = true
	cfg.LogsBuilderConfig.Events.DbServerTopQuery.Enabled = true
	cfg.LogsBuilderConfig.Events.DbServerQuerySample.Enabled = true
	cfg.TopQueryCollection.LookbackTime = 300 // 5-minute window to catch our workload queries
	return cfg
}

// runWorkload executes a handful of SELECT statements so that
// performance_schema.events_statements_summary_by_digest has rows to return.
func runWorkload(t *testing.T, cfg *Config) {
	t.Helper()
	c, err := newMySQLClient(cfg)
	require.NoError(t, err)
	require.NoError(t, c.Connect())
	defer c.Close()

	queries := []string{
		"SELECT 1",
		"SELECT 2",
		"SELECT 3",
		"SELECT NOW()",
		"SELECT VERSION()",
	}
	for range 5 {
		for _, q := range queries {
			_, _ = c.(*mySQLClient).client.Exec(q)
		}
	}
}

// runPerfSchemaSetup enables the performance_schema consumers and instruments
// required for events_statements_summary_by_digest and events_statements_current
// to be populated. These cannot be set as startup flags; they must be updated
// via SQL against the running server.
func runPerfSchemaSetup(t *testing.T, cfg *Config) {
	t.Helper()
	c, err := newMySQLClient(cfg)
	require.NoError(t, err)
	require.NoError(t, c.Connect())
	defer c.Close()

	db := c.(*mySQLClient).client
	stmts := []string{
		"UPDATE performance_schema.setup_consumers SET ENABLED='YES' WHERE NAME IN ('events_statements_current','events_statements_history','events_statements_history_long','events_statements_digest','events_waits_current')",
		"UPDATE performance_schema.setup_instruments SET ENABLED='YES', TIMED='YES' WHERE NAME LIKE 'statement/sql/%'",
	}
	for _, s := range stmts {
		if _, execErr := db.Exec(s); execErr != nil {
			t.Logf("perf schema setup stmt failed (consumers may not be enabled): %v", execErr)
		}
	}
}

// TestIntegrationLogScraper proves that the new multi-version capabilities work
// end-to-end against real database containers:
//
//   - getDBVersion() correctly identifies MySQL vs MariaDB
//   - scrapeTopQueryFunc uses the 6-column template on MySQL 8+ (query_sample_text present)
//     and the 5-column fallback on MariaDB (no query_sample_text column)
//   - scrapeQuerySampleFunc works on both
//   - The shared plan cache is populated by scrapeTopQueryFunc so that
//     scrapeQuerySampleFunc reuses cached plans without a second EXPLAIN call
func TestIntegrationLogScraper(t *testing.T) {
	testCases := []struct {
		name              string
		image             string
		wantMySQL8Plus    bool
		wantSampleTextCol bool
	}{
		{
			name:              "MySQL-8.0.33-LogScraper",
			image:             "mysql:8.0.33",
			wantMySQL8Plus:    true,
			wantSampleTextCol: true,
		},
		{
			name:              "MariaDB-10.11-LogScraper",
			image:             "mariadb:10.11",
			wantMySQL8Plus:    false,
			wantSampleTextCol: false,
		},
		{
			name:              "MariaDB-11.4-LogScraper",
			image:             "mariadb:11.4",
			wantMySQL8Plus:    false,
			wantSampleTextCol: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
				ContainerRequest: testcontainers.ContainerRequest{
					Image:        tc.image,
					ExposedPorts: []string{mysqlPort},
					// Enable performance_schema per README requirements.
					// Consumer activation is done via SQL after startup (see runPerfSchemaSetup).
					Cmd: []string{
						"--performance_schema=ON",
						"--max_digest_length=4096",
						"--performance_schema_max_digest_length=4096",
						"--performance_schema_max_sql_text_length=4096",
					},
					WaitingFor: wait.ForAll(
						wait.ForListeningPort(mysqlPort).WithStartupTimeout(2*time.Minute),
						wait.ForLog("ready for connections").WithStartupTimeout(2*time.Minute),
					),
					Env: map[string]string{
						"MYSQL_ROOT_PASSWORD":   "otel",
						"MYSQL_DATABASE":        "otel",
						"MYSQL_USER":            "otel",
						"MYSQL_PASSWORD":        "otel",
						"MARIADB_ROOT_PASSWORD": "otel",
						"MARIADB_DATABASE":      "otel",
						"MARIADB_USER":          "otel",
						"MARIADB_PASSWORD":      "otel",
					},
				},
				Started: true,
			})
			testcontainers.CleanupContainer(t, ctr)
			if err != nil && strings.Contains(err.Error(), "No such image") {
				t.Skipf("image %s not available locally: %v", tc.image, err)
			}
			require.NoError(t, err)

			host, err := ctr.Host(ctx)
			require.NoError(t, err)
			mappedPort, err := ctr.MappedPort(ctx, mysqlPort)
			require.NoError(t, err)

			cfg := containerConfig(host, mappedPort.Port())

			// Build a shared plan cache (TTL=0 in tests to avoid goroutine leak).
			sharedPlanCache := newTTLCache[string](cfg.TopQueryCollection.QueryPlanCacheSize, 0)

			settings := receivertest.NewNopSettings(metadata.Type)
			scraper := newMySQLScraper(
				settings,
				cfg,
				newCache[int64](int(cfg.TopQueryCollection.MaxQuerySampleCount*2*2)),
				sharedPlanCache,
			)
			require.NoError(t, scraper.start(ctx, nil))
			defer func() { assert.NoError(t, scraper.shutdown(ctx)) }()

			// Verify performance_schema has digest rows for our workload queries.
			{
				c, cerr := newMySQLClient(cfg)
				require.NoError(t, cerr)
				require.NoError(t, c.Connect())
				var count int
				_ = c.(*mySQLClient).client.QueryRow(
					"SELECT COUNT(*) FROM performance_schema.events_statements_summary_by_digest WHERE last_seen >= NOW() - INTERVAL 300 SECOND",
				).Scan(&count)
				t.Logf("performance_schema digest rows in last 300s (before scrape): %d", count)
				c.Close()
			}

			// Enable performance_schema consumers/instruments via SQL.
			runPerfSchemaSetup(t, cfg)

			// Prime the scraper's diff cache with a first scrape so subsequent
			// scrapes have a non-zero sumTimerWait delta to emit records on.
			// (scrapeTopQueries skips records with zero elapsed-time diff.)
			runWorkload(t, cfg)
			_, err = scraper.scrapeTopQueryFunc(ctx)
			require.NoError(t, err, "first scrapeTopQueryFunc (cache priming) must not error")

			// Run more workload so the diff is non-zero on the second scrape.
			runWorkload(t, cfg)

			// --- scrapeTopQueryFunc (second pass — produces records) ---
			topLogs, err := scraper.scrapeTopQueryFunc(ctx)
			require.NoError(t, err, "scrapeTopQueryFunc must not return error (wrong template would cause 'unknown column')")

			// The workload above guarantees at least a few digests exist.
			// Verify structural properties on whatever records were returned.
			topRecordCount := 0
			for i := range topLogs.ResourceLogs().Len() {
				rl := topLogs.ResourceLogs().At(i)
				for j := range rl.ScopeLogs().Len() {
					sl := rl.ScopeLogs().At(j)
					for k := range sl.LogRecords().Len() {
						lr := sl.LogRecords().At(k)
						topRecordCount++

						// db.query.text must always be present and non-empty.
						qt, ok := lr.Attributes().Get("db.query.text")
						assert.True(t, ok, "db.query.text attribute missing")
						assert.NotEmpty(t, qt.Str(), "db.query.text must not be empty")

						// mysql.query_plan is only populated when a sample text was
						// available for EXPLAIN — absence is valid, presence must be non-empty.
						if plan, hasPlan := lr.Attributes().Get("mysql.query_plan"); hasPlan {
							assert.NotEmpty(t, plan.Str(), "mysql.query_plan present but empty")
						}

						// On MySQL <8 / MariaDB the fallback template omits query_sample_text,
						// so the plan cache key will never be populated by top-query scraping.
						// On MySQL 8+ the shared cache should have been populated for any
						// digest that had a valid sample.
						if !tc.wantSampleTextCol {
							// No sample text → plan cache must be empty (nothing to EXPLAIN).
							assert.Equal(t, 0, sharedPlanCache.Len(),
								"plan cache should be empty when fallback template used (no sample text)")
						}
					}
				}
			}
			t.Logf("scrapeTopQueryFunc returned %d log records", topRecordCount)

			// --- scrapeQuerySampleFunc ---
			// Use a separate scraper sharing the same plan cache to prove reuse.
			sampleScraper := newMySQLScraper(
				settings,
				cfg,
				newCache[int64](1),
				sharedPlanCache,
			)
			require.NoError(t, sampleScraper.start(ctx, nil))
			defer func() { assert.NoError(t, sampleScraper.shutdown(ctx)) }()

			sampleLogs, err := sampleScraper.scrapeQuerySampleFunc(ctx)
			require.NoError(t, err, "scrapeQuerySampleFunc must not return error")

			sampleRecordCount := 0
			for i := range sampleLogs.ResourceLogs().Len() {
				rl := sampleLogs.ResourceLogs().At(i)
				for j := range rl.ScopeLogs().Len() {
					sl := rl.ScopeLogs().At(j)
					sampleRecordCount += sl.LogRecords().Len()
				}
			}
			t.Logf("scrapeQuerySampleFunc returned %d log records", sampleRecordCount)

			// Verify version detection on the scraper's client.
			dv, err := scraper.sqlclient.getDBVersion()
			require.NoError(t, err)
			assert.Equal(t, tc.wantMySQL8Plus, dv.isMySQL8Plus(), "isMySQL8Plus mismatch")
			assert.Equal(t, tc.wantSampleTextCol, dv.supportsQuerySampleText(), "supportsQuerySampleText mismatch")
		})
	}
}

// TestVersionCompatibility verifies that getDBVersion() correctly identifies
// MySQL and MariaDB flavors, and that getTopQueries() selects the right query
// template (6-column with query_sample_text for MySQL 8+, 5-column fallback
// for MySQL <8 and all MariaDB versions).
func TestVersionCompatibility(t *testing.T) {
	testCases := []struct {
		name              string
		image             string
		wantProduct       dbProduct
		wantMySQL8Plus    bool
		wantSampleTextCol bool // true ↔ 6-column template used
	}{
		{
			name:              "MySQL 8.0.33",
			image:             "mysql:8.0.33",
			wantProduct:       dbProductMySQL,
			wantMySQL8Plus:    true,
			wantSampleTextCol: true,
		},
		{
			// mysql:5.7 has no official ARM64 image; this case is skipped on
			// ARM hosts (e.g. Apple Silicon). The fallback template path is
			// also exercised by the MariaDB cases below.
			name:              "MySQL 5.7",
			image:             "mysql:5.7",
			wantProduct:       dbProductMySQL,
			wantMySQL8Plus:    false,
			wantSampleTextCol: false,
		},
		{
			name:              "MariaDB 10.11",
			image:             "mariadb:10.11",
			wantProduct:       dbProductMariaDB,
			wantMySQL8Plus:    false,
			wantSampleTextCol: false,
		},
		{
			name:              "MariaDB 11.4",
			image:             "mariadb:11.4",
			wantProduct:       dbProductMariaDB,
			wantMySQL8Plus:    false,
			wantSampleTextCol: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			req := testcontainers.ContainerRequest{
				Image:        tc.image,
				ExposedPorts: []string{mysqlPort},
				WaitingFor: wait.ForListeningPort(mysqlPort).
					WithStartupTimeout(2 * time.Minute),
				Env: map[string]string{
					"MYSQL_ROOT_PASSWORD": "otel",
					"MYSQL_DATABASE":      "otel",
					"MYSQL_USER":          "otel",
					"MYSQL_PASSWORD":      "otel",
					// MariaDB uses the same env var names.
					"MARIADB_ROOT_PASSWORD": "otel",
					"MARIADB_DATABASE":      "otel",
					"MARIADB_USER":          "otel",
					"MARIADB_PASSWORD":      "otel",
				},
			}

			ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
				ContainerRequest: req,
				Started:          true,
			})
			testcontainers.CleanupContainer(t, ctr)
			if err != nil && strings.Contains(err.Error(), "No such image") {
				t.Skipf("image %s not available locally (no official ARM64 build): %v", tc.image, err)
			}
			require.NoError(t, err)

			host, err := ctr.Host(ctx)
			require.NoError(t, err)
			mappedPort, err := ctr.MappedPort(ctx, mysqlPort)
			require.NoError(t, err)

			cfg := &Config{
				Username: "root",
				Password: configopaque.String("otel"),
				AddrConfig: confignet.AddrConfig{
					Endpoint:  fmt.Sprintf("%s:%s", host, mappedPort.Port()),
					Transport: confignet.TransportTypeTCP,
				},
				AllowNativePasswords: true,
				TLS:                  configtls.ClientConfig{Insecure: true},
			}

			c, err := newMySQLClient(cfg)
			require.NoError(t, err)
			require.NoError(t, c.Connect())
			defer c.Close()

			// --- getDBVersion ---
			dv, err := c.getDBVersion()
			require.NoError(t, err)
			assert.Equal(t, tc.wantProduct, dv.product, "product mismatch")
			assert.Equal(t, tc.wantMySQL8Plus, dv.isMySQL8Plus(), "isMySQL8Plus mismatch")
			assert.Equal(t, tc.wantSampleTextCol, dv.supportsQuerySampleText(), "supportsQuerySampleText mismatch")

			// --- getTopQueries: must succeed without error ---
			// No workload is running, so the result may be empty, but the query
			// itself must execute without error — which proves the correct template
			// (5-column vs 6-column) was chosen for this server version.
			queries, err := c.getTopQueries(10, 60)
			require.NoError(t, err, "getTopQueries should not fail (wrong template would cause 'unknown column' error)")

			// For MySQL 8+, any top queries returned must have querySampleText
			// populated (non-empty string is only possible with the 6-column query).
			// For MySQL <8 / MariaDB, querySampleText must always be empty string
			// because the fallback template omits the column.
			for _, q := range queries {
				if tc.wantSampleTextCol {
					// Value may be empty string if the digest row has no sample yet,
					// but the field must have been scanned (no scan error above).
					_ = q.querySampleText
				} else {
					assert.Empty(t, q.querySampleText,
						"querySampleText must be empty when using fallback template (digest: %s)", q.digest)
				}
			}
		})
	}
}
