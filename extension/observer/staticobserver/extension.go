// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package staticobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/staticobserver"

import (
	"context"
	"sync"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
)

var (
	_ extension.Extension = (*staticObserver)(nil)
	_ observer.Observable = (*staticObserver)(nil)
)

// staticObserver fires a single synthetic endpoint of type "static" on ListAndWatch,
// enabling receiver_creator to start statically-configured subreceivers immediately.
type staticObserver struct {
	logger    *zap.Logger
	mu        sync.Mutex
	notifiers []observer.Notify
}

func (*staticObserver) Start(context.Context, component.Host) error { return nil }
func (*staticObserver) Shutdown(context.Context) error              { return nil }

// ListAndWatch registers the notifier, then immediately calls OnAdd with a single synthetic
// static endpoint. Registering before firing ensures the notifier can be unsubscribed from
// within an OnAdd callback.
//
// Note: unlike the general Observable contract ("endpoint synchronization happens
// asynchronously"), this implementation calls OnAdd synchronously before returning, as there
// is no background discovery process.
func (s *staticObserver) ListAndWatch(notify observer.Notify) {
	s.mu.Lock()
	s.notifiers = append(s.notifiers, notify)
	s.mu.Unlock()

	notify.OnAdd([]observer.Endpoint{
		{
			ID:      observer.EndpointID("static-0"),
			Target:  "",
			Details: &observer.Static{},
		},
	})
}

// Unsubscribe removes a previously registered notifier.
func (s *staticObserver) Unsubscribe(notify observer.Notify) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, n := range s.notifiers {
		if n.ID() == notify.ID() {
			s.notifiers = append(s.notifiers[:i], s.notifiers[i+1:]...)
			return
		}
	}
	s.logger.Warn("Unsubscribe called for unknown notifier", zap.String("id", string(notify.ID())))
}
