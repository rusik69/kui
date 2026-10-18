package web

import (
	"context"
	"encoding/json"
	"github.com/rusik69/kui/internal/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func metadataPod(name, version string, labels map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": name, "namespace": "team", "resourceVersion": version, "labels": labels}}}
}

func TestMetadataPreviewIsExactAndMutationReportsPartialConflict(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(),
		metadataPod("one", "7", map[string]any{}), metadataPod("two", "8", map[string]any{}))
	conflictOnce := true
	client.PrependReactor("patch", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		patch := action.(ktesting.PatchAction).GetPatch()
		var body map[string]any
		if err := json.Unmarshal(patch, &body); err != nil {
			t.Fatal(err)
		}
		if body["metadata"].(map[string]any)["resourceVersion"] == "8" && conflictOnce {
			conflictOnce = false
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, "two", context.DeadlineExceeded)
		}
		return false, nil, nil
	})
	clients := &k8s.Clients{Dynamic: client}
	parsed := []metadataPath{{"/c/ctx/team/pods/one", "team", "pods", "one"}, {"/c/ctx/team/pods/two", "team", "pods", "two"}}
	req := metadataRequest{Kind: "labels", Action: "add", Key: "team.example.com/owner", Value: "platform", Preview: true}
	preview := performMetadataBatch(context.Background(), clients, req, parsed)
	if len(preview) != 2 || !preview[0].Success || !preview[1].Success {
		t.Fatalf("preview outcomes = %#v", preview)
	}
	patch, ok := preview[0].Patch.(map[string]any)
	if !ok || patch["metadata"].(map[string]any)["resourceVersion"] != "7" || patch["metadata"].(map[string]any)["labels"].(map[string]any)[req.Key] != req.Value {
		t.Fatalf("preview patch = %#v", preview[0].Patch)
	}
	if len(client.Actions()) != 2 {
		t.Fatalf("preview unexpectedly mutated: %d actions", len(client.Actions()))
	}

	req.Preview = false
	result := performMetadataBatch(context.Background(), clients, req, parsed)
	if len(result) != 2 || !result[0].Success || result[1].Success || result[1].Error == "" {
		t.Fatalf("partial outcomes = %#v", result)
	}
	stored, err := client.Resource(k8s.Registry[0].GVR).Namespace("team").Get(context.Background(), "one", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stored.GetLabels()[req.Key] != req.Value {
		t.Fatalf("successful item metadata = %#v", stored.GetLabels())
	}
	beforeRetryActions := len(client.Actions())
	retry := performMetadataBatch(context.Background(), clients, req, parsed[1:])
	if len(retry) != 1 || !retry[0].Success {
		t.Fatalf("failure-only retry = %#v", retry)
	}
	if len(client.Actions())-beforeRetryActions != 2 {
		t.Fatalf("retry did not target only failed pod: actions %d", len(client.Actions())-beforeRetryActions)
	}
	stored, err = client.Resource(k8s.Registry[0].GVR).Namespace("team").Get(context.Background(), "one", metav1.GetOptions{})
	if err != nil || stored.GetLabels()[req.Key] != req.Value {
		t.Fatalf("retry changed prior success: labels=%v err=%v", stored.GetLabels(), err)
	}
}

func TestBatchMetadataRejectsInvalidPayloadAndPathBeforeClusterLookup(t *testing.T) {
	server := &Server{}
	for _, body := range []string{
		`{"paths":["/c/ctx/team/pods/a"],"kind":"labels","action":"add","key":"bad key","value":"x"}`,
		`{"paths":["/c/other/team/pods/a"],"kind":"labels","action":"add","key":"valid","value":"x"}`,
		`{"paths":["/c/ctx/team/pods/a","/c/ctx/team/pods/a"],"kind":"labels","action":"add","key":"valid","value":"x"}`,
		`{"paths":["/c/ctx/team/pods/a"],"kind":"labels","action":"update","key":"valid","value":"x","extra":true}`,
	} {
		req := httptest.NewRequest("POST", "/c/ctx/bulk-metadata", strings.NewReader(body))
		req.SetPathValue("ctx", "ctx")
		recorder := httptest.NewRecorder()
		server.handleBulkMetadata(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("payload %s status = %d, want 400", body, recorder.Code)
		}
	}
}
