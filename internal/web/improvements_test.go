package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rusik69/kui/internal/k8s"
)

func TestWebSocketUpgraderRejectsCrossOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = upgrader.Upgrade(w, r, nil)
	}))
	defer server.Close()

	dialer := websocket.Dialer{}
	headers := http.Header{"Origin": {"http://untrusted.example"}}
	conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), headers)
	if conn != nil {
		conn.Close()
		t.Fatal("cross-origin websocket connection unexpectedly succeeded")
	}
	if err == nil {
		t.Fatal("expected cross-origin websocket request to fail")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("response = %v, want HTTP %d", response, http.StatusForbidden)
	}
}

func TestValidateYAMLIdentity(t *testing.T) {
	pod := k8s.ResourceKind{Kind: "Pod", Namespaced: true}
	tests := []struct {
		name       string
		kind       string
		objName    string
		namespace  string
		targetName string
		resource   k8s.ResourceKind
		wantError  bool
	}{
		{name: "matching namespaced object", kind: "Pod", objName: "web", namespace: "default", resource: pod},
		{name: "missing kind", objName: "web", namespace: "default", resource: pod, wantError: true},
		{name: "kind mismatch", kind: "Secret", objName: "web", namespace: "default", resource: pod, wantError: true},
		{name: "missing name", kind: "Pod", namespace: "default", resource: pod, wantError: true},
		{name: "name mismatch", kind: "Pod", objName: "other", namespace: "default", resource: pod, wantError: true},
		{name: "missing namespace", kind: "Pod", objName: "web", resource: pod, wantError: true},
		{name: "namespace mismatch", kind: "Pod", objName: "web", namespace: "other", resource: pod, wantError: true},
		{name: "cluster-scoped with namespace", kind: "Node", objName: "node-a", namespace: "default", resource: k8s.ResourceKind{Kind: "Node"}, wantError: true},
		{name: "matching cluster-scoped object", kind: "Node", objName: "node-a", targetName: "node-a", resource: k8s.ResourceKind{Kind: "Node"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]any{
				"kind": tt.kind,
				"metadata": map[string]any{
					"name":      tt.objName,
					"namespace": tt.namespace,
				},
			}}
			targetName := tt.targetName
			if targetName == "" {
				targetName = "web"
			}
			err := validateYAMLIdentity(obj, tt.resource, "default", targetName)
			if (err != nil) != tt.wantError {
				t.Fatalf("validateYAMLIdentity() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestParseFormLimitEnforcesBodyBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		want bool
	}{
		{name: "below limit", size: 15, want: true},
		{name: "exact limit", size: 16, want: true},
		{name: "above limit", size: 17, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "data=" + strings.Repeat("x", tc.size-5)
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			got := parseFormLimit(recorder, req, 16)
			if got != tc.want {
				t.Fatalf("parseFormLimit() = %v, want %v (body bytes = %d)", got, tc.want, len(body))
			}
			if !tc.want && recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
			}
		})
	}
}

func TestParseResourcePathRejectsMalformedSegments(t *testing.T) {
	valid := "/c/context/default/pods/web"
	ns, resource, name, ok := parseResourcePath("context", valid)
	if !ok || ns != "default" || resource != "pods" || name != "web" {
		t.Fatalf("parseResourcePath valid path = %q, %q, %q, %v", ns, resource, name, ok)
	}
	for _, path := range []string{
		"/c/other/default/pods/web",
		"/c/context/default/pods/../web",
		"/c/context/default/pods/",
		"/c/context/default/pods/web?evict=true",
		"/c/context/default//web",
	} {
		t.Run(url.QueryEscape(path), func(t *testing.T) {
			if _, _, _, ok := parseResourcePath("context", path); ok {
				t.Errorf("parseResourcePath(%q) unexpectedly succeeded", path)
			}
		})
	}
}
