package web

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rusik69/kui/internal/k8s"
)

func podWithState(name string, waitingReason string) unstructured.Unstructured {
	status := map[string]any{"phase": "Running"}
	if waitingReason != "" {
		status["containerStatuses"] = []any{
			map[string]any{"state": map[string]any{"waiting": map[string]any{"reason": waitingReason}}},
		}
	}
	return unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name, "namespace": "default"},
		"status":   status,
	}}
}

func TestCountsFromFanOut(t *testing.T) {
	podsRK, _ := k8s.ByName("pods")
	svcRK, _ := k8s.ByName("services")

	results := []k8s.KindResult{
		{
			Kind: podsRK,
			List: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
				podWithState("healthy", ""),
				podWithState("crashy", "CrashLoopBackOff"),
			}},
		},
		{
			Kind: svcRK,
			List: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
		},
		{
			// a kind the current token couldn't list — must be skipped, not
			// surfaced as a broken entry or a panic.
			Kind: mustResourceKind(t, "configmaps"),
			Err:  errListFailed,
		},
	}

	counts := countsFromFanOut(results)

	byName := make(map[string]ObjectCount, len(counts))
	for _, oc := range counts {
		byName[oc.Name] = oc
	}

	pods, ok := byName["pods"]
	if !ok {
		t.Fatal("expected a pods entry")
	}
	if pods.Count != 2 {
		t.Errorf("expected pods count 2, got %d", pods.Count)
	}
	if !pods.Failing {
		t.Error("expected pods.Failing=true due to the CrashLoopBackOff pod")
	}
	if pods.BarPercent != 100 {
		t.Errorf("pods.BarPercent = %d, want 100", pods.BarPercent)
	}

	svc, ok := byName["services"]
	if !ok {
		t.Fatal("expected a services entry")
	}
	if svc.Count != 0 || svc.Failing || svc.BarPercent != 0 {
		t.Errorf("expected services count=0 failing=false bar=0, got %+v", svc)
	}

	if _, ok := byName["configmaps"]; ok {
		t.Error("expected the errored kind to be skipped entirely, not included")
	}
}

func mustResourceKind(t *testing.T, name string) k8s.ResourceKind {
	t.Helper()
	rk, ok := k8s.ByName(name)
	if !ok {
		t.Fatalf("expected %q to be a known registry kind", name)
	}
	return rk
}

var errListFailed = errors.New("list failed")
