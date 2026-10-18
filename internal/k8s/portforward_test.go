package k8s

import "testing"

func TestForwardManagerStopAllClosesAndRemovesEveryHandle(t *testing.T) {
	manager := NewForwardManager()
	first := &ForwardHandle{ID: "pf-1", stopCh: make(chan struct{})}
	second := &ForwardHandle{ID: "pf-2", stopCh: make(chan struct{})}
	manager.live[first.ID] = first
	manager.live[second.ID] = second

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
	manager.StopAll()
}
