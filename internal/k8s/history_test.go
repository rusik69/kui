package k8s

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestDeploymentHistoryListsReplicaSetsWithDeploymentSelector(t *testing.T) {
	newObject := func(kind, name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       kind,
			"metadata":   map[string]interface{}{"name": name, "namespace": "default"},
		}}
	}
	deployment := newObject("Deployment", "demo")
	deployment.SetUID(types.UID("deployment-uid"))
	deployment.Object["spec"] = map[string]interface{}{
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{"app": "demo"},
			"matchExpressions": []interface{}{map[string]interface{}{
				"key": "tier", "operator": "In", "values": []interface{}{"frontend", "worker"},
			}},
		},
	}

	replicaSet := newObject("ReplicaSet", "demo-123")
	replicaSet.SetOwnerReferences([]metav1.OwnerReference{{UID: types.UID("deployment-uid")}})
	replicaSet.SetLabels(map[string]string{"app": "demo", "tier": "frontend"})
	replicaSet.SetAnnotations(map[string]string{revisionAnnotation: "4"})
	replicaSet.Object["spec"] = map[string]interface{}{
		"replicas": int64(2),
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": []interface{}{map[string]interface{}{"image": "example/app:v4"}},
			},
		},
	}

	deployments, _ := ByName("deployments")
	replicaSets, _ := ByName("replicasets")
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		deployments.GVR: "DeploymentList",
		replicaSets.GVR: "ReplicaSetList",
	}, deployment, replicaSet)
	var gotSelector labels.Selector
	dynamicClient.PrependReactor("list", "replicasets", func(action ktesting.Action) (bool, runtime.Object, error) {
		listAction, ok := action.(ktesting.ListAction)
		if !ok {
			t.Fatalf("list action has type %T", action)
		}
		gotSelector = listAction.GetListRestrictions().Labels
		return false, nil, nil
	})

	clients := &Clients{Dynamic: dynamicClient}
	revisions, err := DeploymentHistory(context.Background(), clients, "default", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if gotSelector == nil || !gotSelector.Matches(labels.Set{"app": "demo", "tier": "frontend"}) || gotSelector.Matches(labels.Set{"app": "demo", "tier": "backend"}) {
		t.Fatalf("ReplicaSet list selector = %v, does not match Deployment selector", gotSelector)
	}
	if len(revisions) != 1 || revisions[0].Revision != 4 || !revisions[0].IsCurrent {
		t.Fatalf("DeploymentHistory() = %+v, want current revision 4 (deployment UID %q; ReplicaSet owners %+v)", revisions, deployment.GetUID(), replicaSet.GetOwnerReferences())
	}
}
