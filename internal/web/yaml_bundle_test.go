package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestSelectedYAMLBundleIsStableAndOmitsManagedFields(t *testing.T) {
	pod := func(name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name": name, "namespace": "team", "managedFields": []any{map[string]any{"manager": "test"}},
			},
		}}
	}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), pod("alpha"), pod("bravo"))
	clients := &k8s.Clients{Dynamic: client}
	paths := []metadataPath{
		{path: "/c/context/team/pods/bravo", ns: "team", resource: "pods", name: "bravo"},
		{path: "/c/context/team/pods/alpha", ns: "team", resource: "pods", name: "alpha"},
	}

	first, err := buildSelectedYAMLBundle(context.Background(), clients, paths)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildSelectedYAMLBundle(context.Background(), clients, []metadataPath{paths[1], paths[0]})
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("bundle changed with selection order:\n%s\n---\n%s", first, second)
	}
	text := string(first)
	if strings.Count(text, "\n---\n") != 1 || !strings.HasPrefix(text, "---\n") {
		t.Fatalf("expected two YAML documents, got:\n%s", text)
	}
	if strings.Index(text, "name: alpha") > strings.Index(text, "name: bravo") {
		t.Fatalf("resources are not ordered by path:\n%s", text)
	}
	if strings.Contains(text, "managedFields") {
		t.Fatalf("bundle retained managedFields:\n%s", text)
	}
}

func TestBulkYAMLExportRejectsInvalidSelections(t *testing.T) {
	valid := "/c/context/team/pods/alpha"
	for name, body := range map[string]string{
		"empty":         `{"paths":[]}`,
		"duplicate":     `{"paths":["` + valid + `","` + valid + `"]}`,
		"wrong context": `{"paths":["/c/other/team/pods/alpha"]}`,
		"bad shape":     `{"paths":["/c/context/team/pods"]}`,
		"unknown field": `{"paths":["` + valid + `"],"unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/c/context/bulk-yaml", strings.NewReader(body))
			request.SetPathValue("ctx", "context")
			response := httptest.NewRecorder()
			(&Server{}).handleBulkYAMLExport(response, request)
			if response.Code != 400 {
				t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
			}
		})
	}
}
