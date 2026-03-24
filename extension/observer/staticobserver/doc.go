// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:generate make mdatagen

// Package staticobserver implements an observer extension that immediately fires
// a single synthetic endpoint of type "static" on ListAndWatch, enabling the
// receiver_creator to start statically-configured subreceivers with per-instance
// resource_attributes (e.g. service.name) without requiring dynamic discovery.
package staticobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/staticobserver"
