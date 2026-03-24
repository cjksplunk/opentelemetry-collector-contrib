// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package staticobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/staticobserver"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/staticobserver/internal/metadata"
)

// NewFactory creates a factory for the static observer extension.
func NewFactory() extension.Factory {
	return extension.NewFactory(
		metadata.Type,
		createDefaultConfig,
		createExtension,
		metadata.ExtensionStability,
	)
}

func createDefaultConfig() component.Config {
	return &Config{}
}

func createExtension(_ context.Context, _ extension.Settings, _ component.Config) (extension.Extension, error) {
	return &staticObserver{}, nil
}
