// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package staticobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/staticobserver"

import (
	"context"
	"sync"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
)

// StaticEndpointType is the endpoint type reported by the static observer.
const StaticEndpointType observer.EndpointType = "static"

var (
	_ extension.Extension = (*staticObserver)(nil)
	_ observer.Observable = (*staticObserver)(nil)
)

// staticObserver fires a single synthetic endpoint of type "static" on ListAndWatch,
// enabling receiver_creator to start statically-configured subreceivers immediately.
type staticObserver struct {
	mu        sync.Mutex
	notifiers []observer.Notify
}

// staticEndpointDetails implements observer.EndpointDetails for the synthetic static endpoint.
type staticEndpointDetails struct{}

var _ observer.EndpointDetails = (*staticEndpointDetails)(nil)

func (*staticEndpointDetails) Env() observer.EndpointEnv {
	return observer.EndpointEnv{}
}

func (*staticEndpointDetails) Type() observer.EndpointType {
	return StaticEndpointType
}

func (*staticObserver) Start(context.Context, component.Host) error { return nil }
func (*staticObserver) Shutdown(context.Context) error              { return nil }

// ListAndWatch immediately fires OnAdd with a single synthetic static endpoint,
// then stores the notifier for future Unsubscribe calls.
func (s *staticObserver) ListAndWatch(notify observer.Notify) {
	s.mu.Lock()
	s.notifiers = append(s.notifiers, notify)
	s.mu.Unlock()

	notify.OnAdd([]observer.Endpoint{
		{
			ID:      observer.EndpointID("static-0"),
			Target:  "",
			Details: &staticEndpointDetails{},
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
}
