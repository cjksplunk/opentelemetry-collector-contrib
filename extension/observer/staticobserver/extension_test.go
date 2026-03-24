// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package staticobserver

import (
	"testing"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockNotify struct {
	id      observer.NotifyID
	added   []observer.Endpoint
	removed []observer.Endpoint
	changed []observer.Endpoint
}

func (m *mockNotify) ID() observer.NotifyID           { return m.id }
func (m *mockNotify) OnAdd(added []observer.Endpoint) { m.added = append(m.added, added...) }
func (m *mockNotify) OnRemove(r []observer.Endpoint)  { m.removed = append(m.removed, r...) }
func (m *mockNotify) OnChange(c []observer.Endpoint)  { m.changed = append(m.changed, c...) }

func TestListAndWatchFiresStaticEndpoint(t *testing.T) {
	s := &staticObserver{}
	n := &mockNotify{id: "test-notifier"}

	s.ListAndWatch(n)

	require.Len(t, n.added, 1)
	ep := n.added[0]
	assert.Equal(t, observer.EndpointID("static-0"), ep.ID)
	assert.Equal(t, StaticEndpointType, ep.Details.Type())
	assert.Empty(t, ep.Details.Env())
}

func TestUnsubscribeRemovesNotifier(t *testing.T) {
	s := &staticObserver{}
	n := &mockNotify{id: "test-notifier"}

	s.ListAndWatch(n)
	assert.Len(t, s.notifiers, 1)

	s.Unsubscribe(n)
	assert.Empty(t, s.notifiers)
}

func TestMultipleSubscribers(t *testing.T) {
	s := &staticObserver{}
	n1 := &mockNotify{id: "notifier-1"}
	n2 := &mockNotify{id: "notifier-2"}

	s.ListAndWatch(n1)
	s.ListAndWatch(n2)

	assert.Len(t, s.notifiers, 2)
	assert.Len(t, n1.added, 1)
	assert.Len(t, n2.added, 1)

	s.Unsubscribe(n1)
	assert.Len(t, s.notifiers, 1)
	assert.Equal(t, observer.NotifyID("notifier-2"), s.notifiers[0].ID())
}
