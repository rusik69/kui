package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EphemeralDebugPodLabel marks pods kui created for its "Terminal & SSH"
// ephemeral debug shells, so cleanup and kill can target exactly those and
// nothing else.
const (
	EphemeralDebugPodLabelKey   = "kui.dev/ephemeral"
	EphemeralDebugPodLabelValue = "true"
)

// EphemeralDebugPodSelector is the label selector matching kui debug pods.
const EphemeralDebugPodSelector = EphemeralDebugPodLabelKey + "=" + EphemeralDebugPodLabelValue

// DebugPod is one shell container in a kui ephemeral debug pod.
const DebugPodShellImage = "busybox:1.36"

// DebugPodShellSpec builds a corev1 pod spec for an ephemeral debug shell.
// The pod is RestartPolicyNever and sleeps for a day; terminal pods are
// removed before a later shell starts (see CleanupEphemeralDebugPods).
func DebugPodShellSpec(name, namespace, image string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{EphemeralDebugPodLabelKey: EphemeralDebugPodLabelValue},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:    "shell",
				Image:   image,
				Command: []string{"sleep", "86400"},
			}},
		},
	}
}

// NodeDebugShellSpec builds a non-privileged ephemeral pod pinned to nodeName.
// It joins the host namespaces and exposes the host root at /host read-only.
func NodeDebugShellSpec(namespace, nodeName, image string) *corev1.Pod {
	pod := DebugPodShellSpec("", namespace, image)
	pod.GenerateName = "kui-node-shell-"
	pod.Labels["kui.dev/node-shell"] = "true"
	pod.Spec.NodeName = nodeName
	pod.Spec.HostNetwork = true
	pod.Spec.HostPID = true
	pod.Spec.HostIPC = true
	pod.Spec.Tolerations = []corev1.Toleration{{Operator: corev1.TolerationOpExists}}

	automountServiceAccountToken := false
	pod.Spec.AutomountServiceAccountToken = &automountServiceAccountToken
	hostRoot := corev1.HostPathDirectory
	pod.Spec.Volumes = []corev1.Volume{{
		Name: "host-root",
		VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{
			Path: "/", Type: &hostRoot,
		}},
	}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
		Name: "host-root", MountPath: "/host", ReadOnly: true,
	}}
	allowPrivilegeEscalation := false
	pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
	}
	return pod
}

// CleanupEphemeralDebugPods removes finished kui debug pods from the
// namespace. Pending and running pods belong to active sessions and are kept.
// A failure to delete one finished pod is not fatal — the best effort is made
// and the first error is returned for the caller to surface.
func CleanupEphemeralDebugPods(ctx context.Context, c *Clients, namespace string) error {
	list, err := c.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: EphemeralDebugPodSelector,
	})
	if err != nil {
		return fmt.Errorf("list ephemeral debug pods: %w", err)
	}
	var firstErr error
	for i := range list.Items {
		phase := list.Items[i].Status.Phase
		if phase != corev1.PodSucceeded && phase != corev1.PodFailed {
			continue
		}
		if err := c.Clientset.CoreV1().Pods(namespace).Delete(ctx, list.Items[i].Name, metav1.DeleteOptions{}); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("delete finished debug pod %q: %w", list.Items[i].Name, err)
		}
	}
	return firstErr
}

// KillDebugPod deletes a kui ephemeral debug pod by name, refusing to touch
// a pod that does not carry the ephemeral label (guards against a user pod
// whose name happens to match one kui launched).
func KillDebugPod(ctx context.Context, c *Clients, namespace, name string) error {
	pod, err := c.Clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if pod.Labels[EphemeralDebugPodLabelKey] != EphemeralDebugPodLabelValue {
		return fmt.Errorf("refusing to delete %q: not a kui ephemeral debug pod", name)
	}
	return c.Clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}
