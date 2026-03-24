// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package staticobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/staticobserver"

// Config defines configuration for the static observer extension.
// No configuration is required — the extension fires a single synthetic
// endpoint on startup to trigger receiver_creator subreceiver instantiation.
type Config struct {
	// prevent unkeyed literal initialization
	_ struct{}
}
