// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package staticobserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
)

func newTestObserver() *staticObserver {
	return &staticObserver{logger: zap.NewNop()}
}

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
	s := newTestObserver()
	n := &mockNotify{id: "test-notifier"}

	s.ListAndWatch(n)

	require.Len(t, n.added, 1)
	ep := n.added[0]
	assert.Equal(t, observer.EndpointID("static-0"), ep.ID)
	assert.Equal(t, "", ep.Target, "Target must be empty; subreceiver must specify endpoint explicitly")
	assert.Equal(t, observer.StaticType, ep.Details.Type())
	assert.Empty(t, ep.Details.Env())
	assert.Empty(t, n.removed, "OnRemove must not be called by ListAndWatch")
	assert.Empty(t, n.changed, "OnChange must not be called by ListAndWatch")
}

func TestUnsubscribeRemovesNotifier(t *testing.T) {
	s := newTestObserver()
	n := &mockNotify{id: "test-notifier"}

	s.ListAndWatch(n)
	assert.Len(t, s.notifiers, 1)

	s.Unsubscribe(n)
	assert.Empty(t, s.notifiers)
}

func TestUnsubscribeWithNoSubscribers(t *testing.T) {
	s := newTestObserver()
	n := &mockNotify{id: "never-registered"}

	// Must not panic when unsubscribing a notifier that was never registered.
	assert.NotPanics(t, func() { s.Unsubscribe(n) })
	assert.Empty(t, s.notifiers)
}

func TestMultipleSubscribers(t *testing.T) {
	s := newTestObserver()
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

func TestUnsubscribeMiddleElement(t *testing.T) {
	s := newTestObserver()
	n1 := &mockNotify{id: "notifier-1"}
	n2 := &mockNotify{id: "notifier-2"}
	n3 := &mockNotify{id: "notifier-3"}

	s.ListAndWatch(n1)
	s.ListAndWatch(n2)
	s.ListAndWatch(n3)
	require.Len(t, s.notifiers, 3)

	s.Unsubscribe(n2)
	require.Len(t, s.notifiers, 2)
	assert.Equal(t, observer.NotifyID("notifier-1"), s.notifiers[0].ID())
	assert.Equal(t, observer.NotifyID("notifier-3"), s.notifiers[1].ID())
}
