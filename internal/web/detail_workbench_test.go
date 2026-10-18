package web

import (
	"context"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	corefake "k8s.io/client-go/kubernetes/fake"
)

func TestBuildDetailDataUsesLiveOwnerAndObjectIdentity(t *testing.T) {
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	owner := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "owner", "namespace": "team", "uid": "owner-uid"}}}
	object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{
		"name": "child", "namespace": "team", "uid": "child-uid", "resourceVersion": "42",
		"ownerReferences": []any{map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "owner", "uid": "owner-uid"}},
		"labels":          map[string]any{"app": "worker"}, "annotations": map[string]any{"note": "line one\nline two"},
	}}}
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "ConfigMapList"}, owner, object)
	clients := &k8s.Clients{Dynamic: dynamic, Clientset: corefake.NewSimpleClientset()}
	rk, ok := k8s.ByName("configmaps")
	if !ok {
		t.Fatal("configmaps resource kind is missing")
	}
	data, err := (&Server{}).buildDetailData(context.Background(), clients, rk, "ctx", "team", object)
	if err != nil {
		t.Fatal(err)
	}
	if data.UID != "child-uid" || data.ResourceVersion != "42" || data.APIVersion != "v1" || data.CanonicalURL != "/c/ctx/team/configmaps/child" {
		t.Fatalf("identity = uid %q apiVersion %q resourceVersion %q URL %q", data.UID, data.APIVersion, data.ResourceVersion, data.CanonicalURL)
	}
	if len(data.Owners) != 1 || data.Owners[0].DetailURL != "/c/ctx/team/configmaps/owner" {
		t.Fatalf("live owner references = %#v", data.Owners)
	}
	if data.Labels["app"] != "worker" || data.Annotations["note"] != "line one\nline two" {
		t.Fatalf("metadata values lost: labels=%v annotations=%v", data.Labels, data.Annotations)
	}
}

func TestBuildDetailDataOmitsMissingOrReplacedOwner(t *testing.T) {
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	owner := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "owner", "namespace": "team", "uid": "replacement-uid"}}}
	object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{
		"name": "child", "namespace": "team", "ownerReferences": []any{map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "owner", "uid": "old-uid"}, map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "missing", "uid": "missing-uid"}},
	}}}
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "ConfigMapList"}, owner, object)
	clients := &k8s.Clients{Dynamic: dynamic, Clientset: corefake.NewSimpleClientset(&corev1.Event{})}
	rk, _ := k8s.ByName("configmaps")
	data, err := (&Server{}).buildDetailData(context.Background(), clients, rk, "ctx", "team", object)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Owners) != 0 {
		t.Fatalf("stale or missing owner should not be linked: %#v", data.Owners)
	}
}
