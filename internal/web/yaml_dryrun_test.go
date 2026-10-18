package web

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
)

func TestYAMLDiffPerformsServerSideDryRun(t *testing.T) {
	for _, tt := range []struct {
		name       string
		statusCode int
		apiBody    string
		wantCode   int
		wantBody   string
	}{
		{
			name:       "successful validation",
			statusCode: http.StatusOK,
			apiBody:    `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"demo","namespace":"default","resourceVersion":"1"}}`,
			wantCode:   http.StatusOK,
			wantBody:   "Server-side dry-run passed",
		},
		{
			name:       "API validation failure",
			statusCode: http.StatusUnprocessableEntity,
			apiBody:    `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"Deployment.apps \"demo\" is invalid: spec.replicas: Invalid value","reason":"Invalid","details":{"name":"demo","group":"apps","kind":"Deployment"},"code":422}`,
			wantCode:   http.StatusUnprocessableEntity,
			wantBody:   "server-side dry-run failed",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var gotDryRun, gotMethod, gotBody string
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotDryRun = r.URL.Query().Get("dryRun")
				gotMethod = r.Method
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read API request: %v", err)
				}
				gotBody = string(body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = fmt.Fprint(w, tt.apiBody)
			}))
			defer apiServer.Close()

			kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %q
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`, apiServer.URL)
			kubeconfigPath := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600); err != nil {
				t.Fatal(err)
			}
			server, err := New(nil)
			if err != nil {
				t.Fatal(err)
			}
			server.mgr = k8s.NewManager(kubeconfigPath)

			original := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: demo\n  namespace: default\n  resourceVersion: '1'\nspec:\n  replicas: 1\n"
			edited := strings.Replace(original, "replicas: 1", "replicas: 2", 1)
			form := url.Values{"original": {original}, "yaml": {edited}}
			req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetPathValue("ctx", encodeCtx("test"))
			req.SetPathValue("ns", "default")
			req.SetPathValue("resource", "deployments")
			req.SetPathValue("name", "demo")
			recorder := httptest.NewRecorder()
			server.handleYAMLDiff(recorder, req)

			if recorder.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d; body: %s", recorder.Code, tt.wantCode, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tt.wantBody) {
				t.Errorf("response body %q does not contain %q", recorder.Body.String(), tt.wantBody)
			}
			if gotMethod != http.MethodPut || gotDryRun != "All" {
				t.Errorf("API request = %s dryRun=%q, want PUT dryRun=All", gotMethod, gotDryRun)
			}
			if !strings.Contains(gotBody, `"replicas":2`) {
				t.Errorf("dry-run request omitted the edited replica count: %s", gotBody)
			}
		})
	}
}
