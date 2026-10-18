package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDebugPodShellSpecUsesConfiguredImage(t *testing.T) {
	const image = "registry.example.test/tools/debug-shell:v2"
	pod := DebugPodShellSpec("kui-debug-test", "default", image)
	if got := pod.Spec.Containers[0].Image; got != image {
		t.Fatalf("debug container image = %q, want %q", got, image)
	}
}

func TestCleanupEphemeralDebugPodsPreservesActiveShells(t *testing.T) {
	pods := make([]runtime.Object, 0, 4)
	for name, phase := range map[string]corev1.PodPhase{
		"pending": corev1.PodPending,
		"running": corev1.PodRunning,
		"done":    corev1.PodSucceeded,
		"failed":  corev1.PodFailed,
	} {
		pod := DebugPodShellSpec("kui-debug-"+name, "default", DebugPodShellImage)
		pod.Status.Phase = phase
		pods = append(pods, pod)
	}
	clientset := fake.NewSimpleClientset(pods...)
	clients := &Clients{Clientset: clientset}
	if err := CleanupEphemeralDebugPods(context.Background(), clients, "default"); err != nil {
		t.Fatalf("CleanupEphemeralDebugPods() error = %v", err)
	}
	for _, name := range []string{"pending", "running"} {
		if _, err := clientset.CoreV1().Pods("default").Get(context.Background(), "kui-debug-"+name, metav1.GetOptions{}); err != nil {
			t.Errorf("active shell %q was deleted: %v", name, err)
		}
	}
	for _, name := range []string{"done", "failed"} {
		_, err := clientset.CoreV1().Pods("default").Get(context.Background(), "kui-debug-"+name, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("finished shell %q remains or lookup failed with non-NotFound error: %v", name, err)
		}
	}
}

func TestKillDebugPodDeletesOnlyKuiShell(t *testing.T) {
	debugPod := DebugPodShellSpec("kui-debug-owned", "default", DebugPodShellImage)
	userPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "kui-debug-user-pod", Namespace: "default"}}
	clientset := fake.NewSimpleClientset(debugPod, userPod)
	clients := &Clients{Clientset: clientset}

	if err := KillDebugPod(context.Background(), clients, "default", debugPod.Name); err != nil {
		t.Fatalf("KillDebugPod() error = %v", err)
	}
	if _, err := clientset.CoreV1().Pods("default").Get(context.Background(), debugPod.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("kui shell remains after stop or lookup failed: %v", err)
	}
	if err := KillDebugPod(context.Background(), clients, "default", userPod.Name); err == nil {
		t.Fatal("KillDebugPod() accepted an unlabelled user pod")
	}
	if _, err := clientset.CoreV1().Pods("default").Get(context.Background(), userPod.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("unlabelled user pod was deleted: %v", err)
	}
}
