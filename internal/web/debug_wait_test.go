package web

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestWaitForDebugPodReturnsWhenRunning(t *testing.T) {
	err := waitForDebugPod(context.Background(), func(context.Context) (*corev1.Pod, error) {
		return &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}}, nil
	})
	if err != nil {
		t.Fatalf("waitForDebugPod() error = %v", err)
	}
}

func TestWaitForDebugPodStopsWhenRequestIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- waitForDebugPod(ctx, func(context.Context) (*corev1.Pod, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			return &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}}, nil
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("debug pod readiness check did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waitForDebugPod() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("readiness polling did not stop after request cancellation")
	}
}
