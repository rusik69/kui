package k8s

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func workloadClients(objects ...any) *Clients {
	kubeObjects := make([]runtime.Object, 0, len(objects))
	for _, object := range objects {
		kubeObjects = append(kubeObjects, object.(runtime.Object))
	}
	return &Clients{Clientset: fake.NewSimpleClientset(kubeObjects...)}
}

func TestScaleDeploymentValidatesBeforePatchAndPreservesPatchValue(t *testing.T) {
	clients := workloadClients(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team"}})
	for _, replicas := range []int32{-1, maxDeploymentReplicas + 1} {
		if err := ScaleDeployment(context.Background(), clients, "team", "api", replicas); err == nil {
			t.Fatalf("ScaleDeployment(%d) accepted", replicas)
		}
	}
	if got := len(clients.Clientset.(*fake.Clientset).Actions()); got != 0 {
		t.Fatalf("invalid values issued %d API actions", got)
	}
	if err := ScaleDeployment(context.Background(), clients, "team", "api", 7); err != nil {
		t.Fatal(err)
	}
	actions := clients.Clientset.(*fake.Clientset).Actions()
	patchAction, ok := actions[0].(ktesting.PatchAction)
	if !ok || patchAction.GetPatchType() != types.MergePatchType {
		t.Fatalf("patch action = %#v", actions[0])
	}
	var payload map[string]map[string]int32
	if err := json.Unmarshal(patchAction.GetPatch(), &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload["spec"]["replicas"]; got != 7 {
		t.Fatalf("replicas patch = %d", got)
	}
}

func TestDeploymentRestartPauseResumeUseExplicitPatches(t *testing.T) {
	clients := workloadClients(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team"}})
	for _, action := range []string{"restart", "pause", "resume"} {
		if err := DeploymentAction(context.Background(), clients, "team", "api", action, 0); err != nil {
			t.Fatal(err)
		}
	}
	actions := clients.Clientset.(*fake.Clientset).Actions()
	if len(actions) != 3 {
		t.Fatalf("actions = %d", len(actions))
	}
	var restart map[string]any
	if err := json.Unmarshal(actions[0].(ktesting.PatchAction).GetPatch(), &restart); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(restart)
	if !strings.Contains(string(encoded), "kubectl.kubernetes.io/restartedAt") || !strings.Contains(string(encoded), "T") {
		t.Fatalf("restart patch lacks fresh timestamp: %s", encoded)
	}
	for i, want := range []bool{true, false} {
		var patch map[string]map[string]bool
		if err := json.Unmarshal(actions[i+1].(ktesting.PatchAction).GetPatch(), &patch); err != nil {
			t.Fatal(err)
		}
		if got := patch["spec"]["paused"]; got != want {
			t.Errorf("%s paused = %v, want %v", []string{"pause", "resume"}[i], got, want)
		}
	}
}

func TestDeploymentRollbackRequiresOwnedAvailableRevision(t *testing.T) {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team", UID: "dep-uid"}}
	foreign := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "team", Annotations: map[string]string{"deployment.kubernetes.io/revision": "3"}, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", UID: "other"}}}}
	owned := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "owned", Namespace: "team", Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"}, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", UID: "dep-uid"}}}, Spec: appsv1.ReplicaSetSpec{Template: appsv1.DeploymentSpec{}.Template}}
	clients := workloadClients(deployment, foreign, owned)
	if err := DeploymentAction(context.Background(), clients, "team", "api", "rollback", 3); err == nil {
		t.Fatal("accepted foreign revision")
	}
	if len(clients.Clientset.(*fake.Clientset).Actions()) != 2 {
		t.Fatal("foreign revision should not patch Deployment")
	}
	if err := DeploymentAction(context.Background(), clients, "team", "api", "rollback", 2); err != nil {
		t.Fatal(err)
	}
	actions := clients.Clientset.(*fake.Clientset).Actions()
	var patch map[string]any
	if err := json.Unmarshal(actions[len(actions)-1].(ktesting.PatchAction).GetPatch(), &patch); err != nil {
		t.Fatal(err)
	}
	if _, ok := patch["spec"].(map[string]any)["template"]; !ok {
		t.Fatalf("rollback patch has no selected revision template: %s", actions[len(actions)-1].(ktesting.PatchAction).GetPatch())
	}
}

func TestDeploymentActionRejectsUnknownAction(t *testing.T) {
	clients := workloadClients()
	if err := DeploymentAction(context.Background(), clients, "team", "api", "delete", 0); err == nil {
		t.Fatal("unsupported action accepted")
	}
	if len(clients.Clientset.(*fake.Clientset).Actions()) != 0 {
		t.Fatal("unsupported action touched API")
	}
}

func TestRestartTimestampAlwaysAdvances(t *testing.T) {
	first := freshRestartTimestamp()
	second := freshRestartTimestamp()
	if first == second {
		t.Fatalf("restart timestamps collided: %q", first)
	}
	if _, err := time.Parse(time.RFC3339Nano, first); err != nil {
		t.Fatalf("timestamp %q is not RFC3339Nano: %v", first, err)
	}
}
