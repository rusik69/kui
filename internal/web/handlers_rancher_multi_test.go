package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
	"github.com/rusik69/kui/internal/settings"
)

// fakeMultiRancher is two Rancher servers with one cluster each, where both
// clusters share the same display name — the collision case that motivates
// per-server context namespacing.
type fakeMultiRancher struct {
	alpha, beta *httptest.Server
}

// newFakeMultiRancher starts two management servers. Each serves one cluster
// ("shared" on both) whose generated kubeconfig points at that server's
// unique API endpoint, so the imported contexts are distinguishable by the
// server they reach.
func newFakeMultiRancher(t *testing.T) *fakeMultiRancher {
	t.Helper()
	f := &fakeMultiRancher{}
	// The API servers answer Kubernetes GETs (used when Clients are built).
	for _, fns := range []*(*httptest.Server){&f.alpha, &f.beta} {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"kind":"NamespaceList","apiVersion":"v1","items":[{"metadata":{"name":"default"}}]}`))
		}))
		t.Cleanup(api.Close)
		*fns = api
	}
	newManagement := func(clusterID, token string, apiURL string) *httptest.Server {
		kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: %s
  cluster:
    server: %s
contexts:
- name: %s
  context:
    cluster: %s
    user: %s
current-context: %s
users:
- name: %s
  user:
    token: %s-token
`, clusterID, apiURL, clusterID, clusterID, clusterID, clusterID, clusterID, clusterID)
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"Unauthorized"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.URL.Path == "/v3/clusters":
				_, _ = w.Write([]byte(`{"type":"collection","data":[{"id":"` + clusterID + `","name":"shared","state":"active"}]}`))
			case r.URL.Path == "/v3/clusters/"+clusterID && r.URL.Query().Get("action") == "generateKubeconfig":
				_ = json.NewEncoder(w).Encode(map[string]string{"type": "generateKubeconfigOutput", "config": kubeconfig})
			default:
				http.Error(w, "not found", http.StatusNotFound)
			}
		}))
	}
	f.alpha = newManagement("c-alpha", "alpha-token", f.alpha.URL)
	f.beta = newManagement("c-beta", "beta-token", f.beta.URL)
	t.Cleanup(f.alpha.Close)
	t.Cleanup(f.beta.Close)
	return f
}

func TestMultiRancherContextsAreNamespacedPerServer(t *testing.T) {
	server, _ := rancherTestServer(t)
	fake := newFakeMultiRancher(t)

	// Connect the first server; with one source the plain shape is kept.
	if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: fake.alpha.URL, Token: "alpha-token"}); rec.Code != http.StatusOK {
		t.Fatalf("alpha save = %d: %s", rec.Code, rec.Body.String())
	}
	contexts, err := server.mgr.ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	if !hasContextName(contexts, "rancher/shared") {
		t.Fatalf("single server must keep plain context names: %+v", names(contexts))
	}

	// Connect the second server; both are now namespaced with their host IDs.
	if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: fake.beta.URL, Token: "beta-token"}); rec.Code != http.StatusOK {
		t.Fatalf("beta save = %d: %s", rec.Code, rec.Body.String())
	}
	contexts, err = server.mgr.ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	alphaID := k8s.RancherSourceID(fake.alpha.URL)
	betaID := k8s.RancherSourceID(fake.beta.URL)
	if !hasContextName(contexts, k8s.RancherContextPrefix+alphaID+"/shared") {
		t.Fatalf("alpha cluster missing namespaced context: %+v", names(contexts))
	}
	if !hasContextName(contexts, k8s.RancherContextPrefix+betaID+"/shared") {
		t.Fatalf("beta cluster missing namespaced context: %+v", names(contexts))
	}
	if hasContextName(contexts, "rancher/shared") {
		t.Fatalf("ambiguous plain name must not survive with two servers: %+v", names(contexts))
	}

	// Both imported contexts resolve to clients pointed at their own server.
	for _, ctxName := range []string{
		k8s.RancherContextPrefix + alphaID + "/shared",
		k8s.RancherContextPrefix + betaID + "/shared",
	} {
		if _, err := server.mgr.Clients(ctxName); err != nil {
			t.Fatalf("clients for %s: %v", ctxName, err)
		}
	}
}

func TestMultiRancherStatusListsEveryServer(t *testing.T) {
	server, _ := rancherTestServer(t)
	fake := newFakeMultiRancher(t)
	for _, conn := range []struct{ url, token string }{
		{fake.alpha.URL, "alpha-token"},
		{fake.beta.URL, "beta-token"},
	} {
		if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: conn.url, Token: conn.token}); rec.Code != http.StatusOK {
			t.Fatalf("save %s = %d: %s", conn.url, rec.Code, rec.Body.String())
		}
	}
	rec := rancherRequest(t, server, http.MethodGet, "/rancher", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp rancherSettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Servers) != 2 {
		t.Fatalf("servers = %+v, want two", resp.Servers)
	}
	seen := map[string]bool{}
	for _, srv := range resp.Servers {
		seen[srv.ID] = true
		if !srv.HasToken {
			t.Errorf("server %+v lost its token flag", srv)
		}
		if srv.Reachable == nil || !*srv.Reachable {
			t.Errorf("server %+v did not report reachable", srv)
		}
		if srv.Clusters != 1 {
			t.Errorf("server %+v clusters = %d, want 1", srv, srv.Clusters)
		}
		// Tokens must never leak into status responses.
		if strings.Contains(srv.URL, "token") {
			t.Errorf("server URL suspicious: %s", srv.URL)
		}
	}
	if !seen[k8s.RancherSourceID(fake.alpha.URL)] || !seen[k8s.RancherSourceID(fake.beta.URL)] {
		t.Fatalf("status servers missing one of the hosts: %+v", resp.Servers)
	}
	// Body must not contain either token.
	if strings.Contains(rec.Body.String(), "alpha-token") || strings.Contains(rec.Body.String(), "beta-token") {
		t.Fatalf("token leaked in status response: %s", rec.Body.String())
	}
}

func TestMultiRancherDisconnectOneKeepsTheOther(t *testing.T) {
	server, _ := rancherTestServer(t)
	fake := newFakeMultiRancher(t)
	for _, conn := range []struct{ url, token string }{
		{fake.alpha.URL, "alpha-token"},
		{fake.beta.URL, "beta-token"},
	} {
		if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: conn.url, Token: conn.token}); rec.Code != http.StatusOK {
			t.Fatalf("save %s = %d: %s", conn.url, rec.Code, rec.Body.String())
		}
	}
	// Disconnect alpha by URL.
	del := httptest.NewRequest(http.MethodDelete, "/rancher", strings.NewReader(`{"disconnect":"`+fake.alpha.URL+`"}`))
	del.Header.Set("Content-Type", "application/json")
	del.Header.Set("Origin", "http://"+del.Host)
	del.Header.Set("X-Kui-CSRF", server.csrfToken)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, del)
	if rec.Code != http.StatusOK {
		t.Fatalf("disconnect = %d: %s", rec.Code, rec.Body.String())
	}
	// Beta remains active and serves its contexts under the namespaced shape.
	if got := len(server.mgr.Ranchers()); got != 1 {
		t.Fatalf("attached sources = %d, want 1", got)
	}
	contexts, err := server.mgr.ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	if !hasContextName(contexts, "rancher/shared") {
		t.Fatalf("after disconnect, the single remaining server must use plain names: %+v", names(contexts))
	}
	// Only beta's entry survives on disk.
	persisted := server.settingsStore.Ranchers()
	if len(persisted) != 1 || persisted[0].Token != "beta-token" {
		t.Fatalf("persisted after disconnect = %+v, want only beta", persisted)
	}
}

func TestMultiRancherDisconnectAllClearsEverything(t *testing.T) {
	server, _ := rancherTestServer(t)
	fake := newFakeMultiRancher(t)
	for _, conn := range []struct{ url, token string }{
		{fake.alpha.URL, "alpha-token"},
		{fake.beta.URL, "beta-token"},
	} {
		if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: conn.url, Token: conn.token}); rec.Code != http.StatusOK {
			t.Fatalf("save %s = %d: %s", conn.url, rec.Code, rec.Body.String())
		}
	}
	if rec := rancherRequest(t, server, http.MethodDelete, "/rancher", nil); rec.Code != http.StatusOK {
		t.Fatalf("disconnect all = %d: %s", rec.Code, rec.Body.String())
	}
	if got := server.mgr.Ranchers(); len(got) != 0 {
		t.Fatalf("sources after disconnect-all: %+v", got)
	}
	if got := server.settingsStore.Ranchers(); len(got) != 0 {
		t.Fatalf("persisted after disconnect-all: %+v", got)
	}
}

func TestMultiRancherBlankTokenReusesThisServersSavedToken(t *testing.T) {
	server, _ := rancherTestServer(t)
	fake := newFakeMultiRancher(t)
	if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: fake.alpha.URL, Token: "alpha-token"}); rec.Code != http.StatusOK {
		t.Fatalf("alpha save = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: fake.beta.URL, Token: "beta-token"}); rec.Code != http.StatusOK {
		t.Fatalf("beta save = %d: %s", rec.Code, rec.Body.String())
	}
	// Re-save alpha with a blank token: it must reuse alpha's saved token,
	// not the token of whichever server was saved last (beta's).
	if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: fake.alpha.URL, Token: "", InsecureSkipTLSVerify: true}); rec.Code != http.StatusOK {
		t.Fatalf("alpha resave = %d: %s", rec.Code, rec.Body.String())
	}
	var persisted *settings.Rancher
	for _, entry := range server.settingsStore.Ranchers() {
		if strings.EqualFold(entry.URL, fake.alpha.URL) {
			copied := entry
			persisted = &copied
		}
	}
	if persisted == nil || persisted.Token != "alpha-token" {
		t.Fatalf("alpha token after blank resave = %+v, want alpha-token", persisted)
	}
	if persisted == nil || persisted.InsecureSkipTLSVerify != true {
		t.Fatalf("alpha settings not updated: %+v", persisted)
	}
}

func TestMultiRancherOneUnreachableServerDoesNotHideTheOther(t *testing.T) {
	server, _ := rancherTestServer(t)
	fake := newFakeMultiRancher(t)
	for _, conn := range []struct{ url, token string }{
		{fake.alpha.URL, "alpha-token"},
		{fake.beta.URL, "beta-token"},
	} {
		if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: conn.url, Token: conn.token}); rec.Code != http.StatusOK {
			t.Fatalf("save %s = %d: %s", conn.url, rec.Code, rec.Body.String())
		}
	}
	// Simulate alpha dying: shut its listener down via CloseClientConnections
	// is not exposed; instead attach a third source whose server is dead.
	dead, err := k8s.NewRancherSource("http://127.0.0.1:1", "dead-token", false)
	if err != nil {
		t.Fatal(err)
	}
	server.mgr.SetRancherSources(append(server.mgr.Ranchers(), dead))
	contexts, err := server.mgr.ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	betaID := k8s.RancherSourceID(fake.beta.URL)
	if !hasContextName(contexts, k8s.RancherContextPrefix+betaID+"/shared") {
		t.Fatalf("beta cluster hidden by a dead server: %+v", names(contexts))
	}
}

func TestRancherSourceIDIsTheHostname(t *testing.T) {
	tests := []struct{ url, want string }{
		{"https://rancher.example.com", "rancher.example.com"},
		{"https://RANCHER.Example.COM/prefix", "rancher.example.com"},
		{"http://rancher.example.com:8443", "rancher.example.com:8443"},
		{"https://rancher.example.com/", "rancher.example.com"},
	}
	for _, tt := range tests {
		if got := k8s.RancherSourceID(tt.url); got != tt.want {
			t.Errorf("RancherSourceID(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

// TestMultiRancherSettingsFileHoldsTheList pins the on-disk shape after two
// connections: a "ranchers" array, no legacy singular field.
func TestMultiRancherSettingsFileHoldsTheList(t *testing.T) {
	server, _ := rancherTestServer(t)
	fake := newFakeMultiRancher(t)
	for _, conn := range []struct{ url, token string }{
		{fake.alpha.URL, "alpha-token"},
		{fake.beta.URL, "beta-token"},
	} {
		if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: conn.url, Token: conn.token}); rec.Code != http.StatusOK {
			t.Fatalf("save %s = %d: %s", conn.url, rec.Code, rec.Body.String())
		}
	}
	set, err := server.settingsStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Ranchers) != 2 || set.Rancher != nil {
		t.Fatalf("settings = %+v, want two entries in ranchers and no legacy field", set)
	}
}

// hasContextName reports whether contexts contains the given name.
func hasContextName(contexts []k8s.ContextInfo, name string) bool {
	for _, ctx := range contexts {
		if ctx.Name == name {
			return true
		}
	}
	return false
}

// names maps contexts to their name list for failure messages.
func names(contexts []k8s.ContextInfo) []string {
	out := make([]string, 0, len(contexts))
	for _, ctx := range contexts {
		out = append(out, ctx.Name)
	}
	return out
}
