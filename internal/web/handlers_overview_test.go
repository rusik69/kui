package web

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/rusik69/kui/internal/k8s"
)

func TestNodeReady(t *testing.T) {
	ready := []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
	notReady := []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}
	if !nodeReady(ready) {
		t.Error("expected ready=true for a True Ready condition")
	}
	if nodeReady(notReady) {
		t.Error("expected ready=false for a False Ready condition")
	}
	if nodeReady(nil) {
		t.Error("expected ready=false with no conditions at all")
	}
}

func TestNsOrAll(t *testing.T) {
	if got := nsOrAll("_all"); got != metav1.NamespaceAll {
		t.Errorf("nsOrAll(\"_all\") = %q, want NamespaceAll", got)
	}
	if got := nsOrAll("default"); got != "default" {
		t.Errorf("nsOrAll(\"default\") = %q, want \"default\"", got)
	}
}

func TestPodStateTypedCrashLoopBackOff(t *testing.T) {
	pod := corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning, // k8s never flips phase to Failed for a crash-looping container
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			}},
		},
	}
	if state := podStateTyped(pod); state != "CrashLoopBackOff" {
		t.Errorf("podStateTyped = %q, want CrashLoopBackOff", state)
	}
	if !podStateIsBad(podStateTyped(pod)) {
		t.Error("expected a CrashLoopBackOff pod to be flagged unhealthy")
	}
}

func TestTallyPodPhasesCountsCrashLoopAsUnhealthy(t *testing.T) {
	pods := []corev1.Pod{
		{Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			}},
		}},
		{Status: corev1.PodStatus{Phase: corev1.PodFailed}},
	}

	tally := tallyPodPhases(pods)

	if tally.Running != 2 {
		t.Errorf("expected 2 pods counted Running by phase, got %d", tally.Running)
	}
	if tally.Failed != 1 {
		t.Errorf("expected 1 pod counted Failed by phase, got %d", tally.Failed)
	}
	// The key assertion: Unhealthy must be 2 (the phase=Failed pod AND the
	// CrashLoopBackOff pod that stays at phase=Running), not 1 — a plain
	// phase==Failed check misses the CrashLoopBackOff case entirely.
	if tally.Unhealthy != 2 {
		t.Errorf("expected 2 unhealthy pods (Failed + CrashLoopBackOff), got %d", tally.Unhealthy)
	}
}

func TestBuildOverviewData(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	healthyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "healthy", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("500m"),
			}},
		}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	crashyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "crashy", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Ready: false,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			}},
		},
	}
	warningEvent := &corev1.Event{
		ObjectMeta:    metav1.ObjectMeta{Name: "evt-1", Namespace: "default"},
		Type:          corev1.EventTypeWarning,
		Reason:        "BackOff",
		Message:       "back-off restarting failed container",
		LastTimestamp: metav1.NewTime(time.Now()),
	}

	clientset := fake.NewSimpleClientset(node, ns, healthyPod, crashyPod, warningEvent)
	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, healthyPod, crashyPod)
	c := &k8s.Clients{Clientset: clientset, Dynamic: dyn}

	s := &Server{}
	data, err := s.buildOverviewData(context.Background(), c, "_all")
	if err != nil {
		t.Fatalf("buildOverviewData: %v", err)
	}

	if data.NodeCount != 1 || data.NodeReadyCount != 1 {
		t.Errorf("expected 1/1 nodes ready, got %d/%d", data.NodeReadyCount, data.NodeCount)
	}
	if data.NamespaceCount != 1 {
		t.Errorf("expected 1 namespace, got %d", data.NamespaceCount)
	}
	if data.PodRunning != 2 {
		t.Errorf("expected 2 pods counted Running by phase (both are phase=Running), got %d", data.PodRunning)
	}
	if data.PodFailed != 0 {
		t.Errorf("expected only phase=Failed pods in the failed tile, got %d", data.PodFailed)
	}
	if data.WarningCount != 1 {
		t.Errorf("expected 1 warning event, got %d", data.WarningCount)
	}
	if data.UpdatedAt.IsZero() {
		t.Error("expected overview refresh timestamp")
	}
	if !data.EventsAvailable {
		t.Error("expected event section to be marked available")
	}
	if len(data.EventGroups) != 1 || data.EventGroups[0].Type != corev1.EventTypeWarning || data.EventGroups[0].Reason != "BackOff" || data.EventGroups[0].Count != 1 {
		t.Errorf("expected one aggregated BackOff event group, got %+v", data.EventGroups)
	}

	if len(data.NeedsAttention) != 1 || data.NeedsAttention[0].Name != "crashy" {
		t.Errorf("expected exactly the crashy pod in NeedsAttention, got %+v", data.NeedsAttention)
	}
	if data.NeedsAttention[0].Reason != "CrashLoopBackOff" {
		t.Errorf("expected NeedsAttention reason CrashLoopBackOff, got %q", data.NeedsAttention[0].Reason)
	}
	if data.NeedsAttention[0].Severity != "critical" {
		t.Errorf("expected CrashLoopBackOff severity critical, got %q", data.NeedsAttention[0].Severity)
	}

	if data.CPUAllocatableMilli != 4000 {
		t.Errorf("expected 4000m CPU allocatable, got %d", data.CPUAllocatableMilli)
	}
	if data.CPURequestedMilli != 500 {
		t.Errorf("expected 500m CPU requested, got %d", data.CPURequestedMilli)
	}

	foundPodsCount := false
	for _, oc := range data.ObjectCounts {
		if oc.Name == "pods" {
			foundPodsCount = true
			if oc.Count != 2 {
				t.Errorf("expected ObjectCounts pods=2, got %d", oc.Count)
			}
			if !oc.Failing {
				t.Error("expected ObjectCounts pods.Failing=true due to the crashy pod")
			}
		}
	}
	if !foundPodsCount {
		t.Error("expected an ObjectCounts entry for pods")
	}
}
