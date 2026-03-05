// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mysqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mysqlreceiver"

import (
	"github.com/DataDog/datadog-agent/pkg/obfuscate"
)

var (
	obfuscateSQLConfig = obfuscate.SQLConfig{DBMS: "mysql"}
	obfuscatorConfig   = obfuscate.Config{
		SQLExecPlan:          defaultSQLPlanObfuscateSettings,
		SQLExecPlanNormalize: defaultSQLPlanNormalizeSettings,
	}
)

type obfuscator obfuscate.Obfuscator

func newObfuscator() *obfuscator {
	return (*obfuscator)(obfuscate.NewObfuscator(obfuscatorConfig))
}

func (o *obfuscator) obfuscateSQLString(sql string) (string, error) {
	obfuscatedQuery, err := (*obfuscate.Obfuscator)(o).ObfuscateSQLStringWithOptions(sql, &obfuscateSQLConfig, "")
	if err != nil {
		return "", err
	}
	return obfuscatedQuery.Query, nil
}

func (o *obfuscator) obfuscatePlan(plan string) (string, error) {
	obfuscated, err := (*obfuscate.Obfuscator)(o).ObfuscateSQLExecPlan(plan, false)
	if err != nil {
		return "", err
	}
	return obfuscated, nil
}

func (o *obfuscator) normalizePlan(plan string) (string, error) {
	normalized, err := (*obfuscate.Obfuscator)(o).ObfuscateSQLExecPlan(plan, true)
	if err != nil {
		return "", err
	}
	return normalized, nil
}

// For further information, see https://dev.mysql.com/doc/refman/8.4/en/explain.html
// MySQL 8.4 EXPLAIN FORMAT=JSON produces two formats depending on explain_json_format_version:
//   - Version 1 (default): query_block → ordering_operation → table → attached_condition
//   - Version 2: top-level query + inputs array, each node has condition/operation/access_type etc.

// defaultSQLPlanNormalizeSettings are the default JSON obfuscator settings for both obfuscating and normalizing SQL
// execution plans.
var defaultSQLPlanNormalizeSettings = obfuscate.JSONConfig{
	Enabled: true,
	ObfuscateSQLValues: []string{
		// v1 and v2: the full query text
		"query",
		// v1: SQL condition expression attached to a table scan
		"attached_condition",
		// V1: costs
		"query_cost",
		"cost_info",
		"read_cost",
		"eval_cost",
		"prefix_cost",
		"data_read_per_join",
		// V1: row estimates
		"rows_examined_per_scan",
		"rows_produced_per_join",
		// v2: SQL condition expression on a filter node
		"condition",
		// v2: human-readable description of a plan node (e.g. "Filter: (...)", "Table scan on ...")
		"operation",
		// V2: costs
		"estimated_total_cost",
		"estimated_rows",
		// V2: row estimates
		"estimated_rows",
	},
	KeepValues: []string{
		// mysql
		"access_type",
		"cost_info",
		"filtered",
		"rows_examined_per_join",
		"rows_examined_per_scan",
		"rows_produced_per_join",
		"select_id",
		"table",
		"table_name",
		"used_columns",
		"using_filesort",
	},
}

// defaultSQLPlanObfuscateSettings builds upon sqlPlanNormalizeSettings by including cost & row estimates in the keep
// list
var defaultSQLPlanObfuscateSettings = obfuscate.JSONConfig{
	Enabled: true,
	ObfuscateSQLValues: []string{
		// v1 and v2: the full query text
		"query",
		// v2: SQL condition expression on a filter node
		"condition",
		// v2: human-readable description of a plan node (e.g. "Filter: (...)", "Table scan on ...")
		"operation",
		// v1: SQL condition expression attached to a table scan
		"attached_condition",
	},
	KeepValues: []string{
		// v1 structural fields
		"cost_info",
		"ordering_operation",
		"query_block",
		"query_plan",
		"query_type",
		"select_id",
		"table",
		"used_columns",
		"using_filesort",
		// v2 structural fields
		"access_type",
		"covering",
		"estimated_rows",
		"estimated_total_cost",
		"filter_columns",
		"index_access_type",
		"index_name",
		"inputs",
		"json_schema_version",
		"limit",
		"limit_offset",
		"per_chunk_limit",
		"ranges",
		"row_ids",
		"schema_name",
		"sort_fields",
		"table_name",
	},
}
