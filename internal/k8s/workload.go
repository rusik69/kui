package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var restartTimestampMu sync.Mutex
var lastRestartTimestamp time.Time

func freshRestartTimestamp() string {
	restartTimestampMu.Lock()
	defer restartTimestampMu.Unlock()
	now := time.Now().UTC()
	if !now.After(lastRestartTimestamp) {
		now = lastRestartTimestamp.Add(time.Nanosecond)
	}
	lastRestartTimestamp = now
	return now.Format(time.RFC3339Nano)
}

const maxDeploymentReplicas int32 = 100000

// ScaleDeployment updates a Deployment's desired replicas using a merge patch.
func ScaleDeployment(ctx context.Context, c *Clients, namespace, name string, replicas int32) error {
	if replicas < 0 || replicas > maxDeploymentReplicas {
		return fmt.Errorf("replicas must be between 0 and %d", maxDeploymentReplicas)
	}
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"replicas": replicas}})
	_, err := c.Clientset.AppsV1().Deployments(namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// DeploymentAction runs a single explicit rollout operation.
func DeploymentAction(ctx context.Context, c *Clients, namespace, name, action string, revision int64) error {
	var patch map[string]any
	switch action {
	case "restart":
		patch = map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"kubectl.kubernetes.io/restartedAt": freshRestartTimestamp()}}}}}
	case "pause":
		patch = map[string]any{"spec": map[string]any{"paused": true}}
	case "resume":
		patch = map[string]any{"spec": map[string]any{"paused": false}}
	case "rollback":
		if revision < 1 {
			return fmt.Errorf("revision must be positive")
		}
		deployment, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		old, err := c.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		var target *appsv1.ReplicaSet
		for i := range old.Items {
			rs := &old.Items[i]
			if rs.Annotations["deployment.kubernetes.io/revision"] == fmt.Sprint(revision) {
				for _, owner := range rs.OwnerReferences {
					if owner.UID == deployment.UID && owner.Kind == "Deployment" {
						target = rs
						break
					}
				}
				if target != nil {
					break
				}
			}
		}
		if target == nil {
			return fmt.Errorf("revision %d is not available for this Deployment", revision)
		}
		patch = map[string]any{"spec": map[string]any{"template": target.Spec.Template}}
	default:
		return fmt.Errorf("unsupported Deployment action %q", action)
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = c.Clientset.AppsV1().Deployments(namespace).Patch(ctx, name, types.MergePatchType, data, metav1.PatchOptions{})
	return err
}
