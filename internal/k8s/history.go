package k8s

import (
	"context"
	"sort"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	rsList, err := List(ctx, c, rsRK, namespace)
	if err != nil {
		return nil, err
	}

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
		image := firstContainerImage(rs.Object)
		revisions = append(revisions, Revision{
			Name:     rs.GetName(),
			Revision: rev,
			Replicas: replicas,
			Created:  rs.GetCreationTimestamp(),
			Image:    image,
		})
	}
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Revision > revisions[j].Revision })
	if len(revisions) > 0 {
		revisions[0].IsCurrent = true
	}
	return revisions, nil
}

func firstContainerImage(obj map[string]interface{}) string {
	containers, ok, _ := unstructured.NestedSlice(obj, "spec", "template", "spec", "containers")
	if !ok || len(containers) == 0 {
		return ""
	}
	c, ok := containers[0].(map[string]interface{})
	if !ok {
		return ""
	}
	img, _ := c["image"].(string)
	return img
}
