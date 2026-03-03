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

// defaultSQLPlanNormalizeSettings are the default JSON obfuscator settings for both obfuscating and normalizing SQL
// execution plans
var defaultSQLPlanNormalizeSettings = obfuscate.JSONConfig{
	Enabled: true,
	ObfuscateSQLValues: []string{
		// mysql
		"query",
		"condition",
		"operation",
	},
	KeepValues: []string{
		// mysql
		"query_plan",
		"query_type",
		"json_schema_version",
	},
}

// defaultSQLPlanObfuscateSettings builds upon sqlPlanNormalizeSettings by including cost & row estimates in the keep
// list
var defaultSQLPlanObfuscateSettings = obfuscate.JSONConfig{
	Enabled: true,
	KeepValues: append([]string{
		// mysql
		"query_plan",
	}, defaultSQLPlanNormalizeSettings.KeepValues...),
	ObfuscateSQLValues: defaultSQLPlanNormalizeSettings.ObfuscateSQLValues,
}
