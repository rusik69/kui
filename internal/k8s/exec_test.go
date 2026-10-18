package k8s

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/tools/remotecommand"
)

func TestFixedSizeQueueStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := &fixedSizeQueue{ctx: ctx, ch: make(chan remotecommand.TerminalSize)}
	result := make(chan *remotecommand.TerminalSize, 1)
	go func() { result <- queue.Next() }()
	cancel()
	select {
	case got := <-result:
		if got != nil {
			t.Fatalf("Next() = %v after cancellation, want nil", got)
		}
	case <-time.After(time.Second):
		t.Fatal("Next() remained blocked after exec cancellation")
	}
}
