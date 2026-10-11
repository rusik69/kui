package web

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestColumnsKnownKinds(t *testing.T) {
	tests := map[string][]string{
		"pods":        {"Name", "Ready", "State", "Restarts", "Node", "Age"},
		"deployments": {"Name", "Ready", "Up-to-date", "Available", "Age"},
		"services":    {"Name", "Type", "Cluster-IP", "Ports", "Age"},
	}
	for kind, want := range tests {
		got := Columns(kind)
		if len(got) != len(want) {
			t.Errorf("Columns(%q) = %v, want %v", kind, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("Columns(%q)[%d] = %q, want %q", kind, i, got[i], want[i])
			}
		}
	}
}

func TestColumnsUnknownKindFallsBack(t *testing.T) {
	got := Columns("some-crd-kind")
	want := []string{"Name", "Kind", "Age"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Columns(unknown) = %v, want fallback %v", got, want)
	}
}

func newPod(name string, containerStatuses []any, nodeName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "default",
		},
		"spec": map[string]any{
			"nodeName": nodeName,
		},
		"status": map[string]any{
			"phase":             "Running",
			"containerStatuses": containerStatuses,
		},
	}}
}

func TestRowCellsPodRunningHealthy(t *testing.T) {
	pod := newPod("healthy", []any{
		map[string]any{"ready": true, "restartCount": int64(0), "state": map[string]any{"running": map[string]any{}}},
	}, "node-1")

	row := RowCells("pods", pod)

	if row.DotClass != "dot-green" {
		t.Errorf("expected dot-green for healthy running pod, got %q", row.DotClass)
	}
	if row.Cells[1] != "1/1" {
		t.Errorf("expected ready 1/1, got %q", row.Cells[1])
	}
	if row.Cells[2] != "Running" {
		t.Errorf("expected state Running, got %q", row.Cells[2])
	}
	if row.CellClasses[3] != "" {
		t.Errorf("expected no restart-count class for 0 restarts, got %q", row.CellClasses[3])
	}
}

// This is the exact bug found during round 3: a CrashLoopBackOff pod reports
// phase=Running in Kubernetes (the phase never becomes Failed), so checking
// phase alone misses it entirely. The dot/row rendering and podStateIsBad
// must catch it via the container's waiting.reason instead.
func TestRowCellsPodCrashLoopBackOff(t *testing.T) {
	pod := newPod("crashy", []any{
		map[string]any{
			"ready": false, "restartCount": int64(6),
			"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
		},
	}, "node-1")

	row := RowCells("pods", pod)

	if row.DotClass != "dot-red" {
		t.Errorf("expected dot-red for CrashLoopBackOff pod (phase stays Running in real k8s), got %q", row.DotClass)
	}
	if row.Cells[2] != "CrashLoopBackOff" {
		t.Errorf("expected state CrashLoopBackOff, got %q", row.Cells[2])
	}
	if row.CellClasses[3] != "cell-danger" {
		t.Errorf("expected cell-danger restart class for 6 restarts, got %q", row.CellClasses[3])
	}
}

func TestRowCellsPodFailed(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "failed", "namespace": "default"},
		"spec":     map[string]any{},
		"status":   map[string]any{"phase": "Failed"},
	}}

	row := RowCells("pods", pod)
	if row.DotClass != "dot-red" {
		t.Errorf("expected dot-red for Failed phase, got %q", row.DotClass)
	}
}

func TestPodStateIsBad(t *testing.T) {
	tests := map[string]bool{
		"Running":             false,
		"Succeeded":           false,
		"Pending":             false,
		"Failed":              true,
		"CrashLoopBackOff":    true,
		"ImagePullBackOff":    true,
		"ContainerError":      true,
		"ErrImagePull":        true,
		"InvalidImageName":    true,
		"OOMKilled":           true,
		"DeadlineExceeded":    true,
		"ContainerCannotRun":  true,
		"ErrImageNeverPull":   true,
		"RegistryUnavailable": true,
	}
	for state, want := range tests {
		if got := podStateIsBad(state); got != want {
			t.Errorf("podStateIsBad(%q) = %v, want %v", state, got, want)
		}
	}
}
