package k8s

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const revisionAnnotation = "deployment.kubernetes.io/revision"

// Revision is one entry in a Deployment's rollout history, derived from its
// owned ReplicaSets (the same source `kubectl rollout history` uses).
type Revision struct {
	Name      string
	Revision  int
	Replicas  int64
	Created   metav1.Time
	Image     string
	IsCurrent bool
}

// DeploymentHistory lists the rollout history for one Deployment by walking
// its owned ReplicaSets and reading their revision annotation.
func DeploymentHistory(ctx context.Context, c *Clients, namespace, deploymentName string) ([]Revision, error) {
	depRK, _ := ByName("deployments")
	dep, err := Get(ctx, c, depRK, namespace, deploymentName)
	if err != nil {
		return nil, err
	}
	currentUID := dep.GetUID()

	rsRK, _ := ByName("replicasets")
	selectorMap, found, err := unstructured.NestedMap(dep.Object, "spec", "selector")
	if err != nil {
		return nil, fmt.Errorf("read deployment selector: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("deployment %q has no selector", deploymentName)
	}
	var deploymentSelector metav1.LabelSelector
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(selectorMap, &deploymentSelector); err != nil {
		return nil, fmt.Errorf("decode deployment selector: %w", err)
	}
	selector, err := metav1.LabelSelectorAsSelector(&deploymentSelector)
	if err != nil {
		return nil, fmt.Errorf("build ReplicaSet selector: %w", err)
	}
	rsList, err := c.Dynamic.Resource(rsRK.GVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}

	currentRevision, _ := strconv.Atoi(dep.GetAnnotations()[revisionAnnotation])
	var revisions []Revision
	for _, rs := range rsList.Items {
		owned := false
		for _, ref := range rs.GetOwnerReferences() {
			if ref.UID == currentUID {
				owned = true
				break
			}
		}
		if !owned {
			continue
		}
		rev, _ := strconv.Atoi(rs.GetAnnotations()[revisionAnnotation])
		replicas, _, _ := unstructured.NestedInt64(rs.Object, "spec", "replicas")
		revisions = append(revisions, Revision{
			Name: rs.GetName(), Revision: rev, Replicas: replicas,
			Created: rs.GetCreationTimestamp(), Image: containerImages(rs.Object),
			IsCurrent: currentRevision != 0 && rev == currentRevision,
		})
	}
	sort.Slice(revisions, func(i, j int) bool {
		if revisions[i].Revision != revisions[j].Revision {
			return revisions[i].Revision > revisions[j].Revision
		}
		if !revisions[i].Created.Equal(&revisions[j].Created) {
			return revisions[i].Created.After(revisions[j].Created.Time)
		}
		return revisions[i].Name < revisions[j].Name
	})
	return revisions, nil
}

func containerImages(obj map[string]interface{}) string {
	containers, _, _ := unstructured.NestedSlice(obj, "spec", "template", "spec", "containers")
	initContainers, _, _ := unstructured.NestedSlice(obj, "spec", "template", "spec", "initContainers")
	images := make([]string, 0, len(containers)+len(initContainers))
	appendImages := func(containers []interface{}) {
		for _, item := range containers {
			container, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if image, ok := container["image"].(string); ok && image != "" {
				images = append(images, image)
			}
		}
	}
	appendImages(containers)
	appendImages(initContainers)
	return strings.Join(images, ", ")
}
