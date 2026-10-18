package k8s

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func TestDebugPodShellSpecUsesConfiguredImage(t *testing.T) {
	const image = "registry.example.test/tools/debug-shell:v2"
	pod := DebugPodShellSpec("kui-debug-test", "default", image)
	if got := pod.Spec.Containers[0].Image; got != image {
		t.Fatalf("debug container image = %q, want %q", got, image)
	}
}

func TestNodeDebugShellSpecUsesHostAccessWithoutPrivilegedContainer(t *testing.T) {
	pod := NodeDebugShellSpec("ops", "worker-1", "registry.example.test/debug:v1")
	if pod.Namespace != "ops" || pod.Spec.NodeName != "worker-1" || !strings.HasPrefix(pod.GenerateName, "kui-node-shell-") {
		t.Fatalf("node shell identity = namespace %q, node %q, prefix %q", pod.Namespace, pod.Spec.NodeName, pod.GenerateName)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("node shell should not receive a service-account token")
	}
	if !pod.Spec.HostNetwork || !pod.Spec.HostPID || !pod.Spec.HostIPC {
		t.Fatalf("node shell does not join host namespaces: %+v", pod.Spec)
	}
	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].HostPath == nil ||
		pod.Spec.Volumes[0].HostPath.Path != "/" || pod.Spec.Volumes[0].HostPath.Type == nil ||
		*pod.Spec.Volumes[0].HostPath.Type != corev1.HostPathDirectory {
		t.Fatalf("host root volume = %+v", pod.Spec.Volumes)
	}
	container := pod.Spec.Containers[0]
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].MountPath != "/host" ||
		container.VolumeMounts[0].Name != "host-root" || !container.VolumeMounts[0].ReadOnly {
		t.Fatalf("host root mount = %+v", container.VolumeMounts)
	}
	if container.SecurityContext == nil {
		t.Fatal("node shell is missing its container security context")
	}
	if container.SecurityContext.Privileged != nil && *container.SecurityContext.Privileged {
		t.Fatal("node shell container must not be privileged")
	}
	if container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("node shell must disable privilege escalation")
	}
	if len(pod.Spec.Tolerations) != 1 || pod.Spec.Tolerations[0].Operator != corev1.TolerationOpExists {
		t.Fatalf("node shell tolerations = %+v", pod.Spec.Tolerations)
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
