package k8s

import (
	"errors"
	"testing"

	"k8s.io/client-go/tools/portforward"
)

func TestForwardManagerStopAllClosesAndRemovesEveryHandle(t *testing.T) {
	manager := NewForwardManager()
	first := &ForwardHandle{ID: "pf-1", Context: "ctx-a", Namespace: "ns", Pod: "a", LocalPort: 8080, RemotePort: 80, stopCh: make(chan struct{})}
	second := &ForwardHandle{ID: "pf-2", Context: "ctx-b", Namespace: "ns", Pod: "b", LocalPort: 8081, RemotePort: 81, stopCh: make(chan struct{})}
	manager.live[first.ID] = first
	manager.live[second.ID] = second

	snapshot := manager.Active()
	if len(snapshot) != 2 || snapshot[0].ID != first.ID || snapshot[0].Context != "ctx-a" || snapshot[0].LocalPort != first.LocalPort || snapshot[1].ID != second.ID || snapshot[1].Context != "ctx-b" {
		t.Fatalf("Active() = %+v, want stable snapshot of both forwards", snapshot)
	}

	manager.StopAll()

	if len(manager.live) != 0 {
		t.Fatalf("live handles = %d, want 0", len(manager.live))
	}
	for _, handle := range []*ForwardHandle{first, second} {
		select {
		case <-handle.stopCh:
		default:
			t.Errorf("stop channel for %s remains open", handle.ID)
		}
	}
	if active := manager.Active(); len(active) != 0 {
		t.Errorf("Active() after StopAll = %+v, want empty snapshot", active)
	}
	manager.StopAll()
}

func TestForwardHandleExposesTerminationError(t *testing.T) {
	want := errors.New("tunnel failed")
	handle := &ForwardHandle{}
	handle.setTerminationError(want)
	if got := handle.TerminationError(); !errors.Is(got, want) {
		t.Fatalf("TerminationError() = %v, want %v", got, want)
	}
}

func TestResolvedLocalPortUsesEphemeralAllocation(t *testing.T) {
	got, err := resolvedLocalPort(0, []portforward.ForwardedPort{{Local: 49152, Remote: 80}})
	if err != nil || got != 49152 {
		t.Fatalf("resolvedLocalPort(0, allocated) = (%d, %v), want allocated local port 49152", got, err)
	}
	if _, err := resolvedLocalPort(0, nil); err == nil {
		t.Fatal("resolvedLocalPort(0, no allocated ports) succeeded, want error")
	}
}
