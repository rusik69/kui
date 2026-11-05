package web

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rusik69/kui/internal/k8s"
	"github.com/rusik69/kui/internal/settings"
)

// Rancher settings payloads. The API token is write-only: responses report
// whether a token is set, never the token itself. The same endpoints carry
// the kubeconfig directory setting.
type rancherSettingsRequest struct {
	URL                   string  `json:"url"`
	Token                 string  `json:"token"`
	InsecureSkipTLSVerify bool    `json:"insecureSkipTLSVerify"`
	KubeconfigDir         *string `json:"kubeconfigDir,omitempty"`
	// Disconnect names the server to remove (by URL). Empty with action
	// empty means "save/connect"; action "disconnect" with an empty URL
	// disconnects every server.
	Disconnect string `json:"disconnect,omitempty"`
}

type rancherSettingsResponse struct {
	Configured bool   `json:"configured"`
	URL        string `json:"url,omitempty"`
	HasToken   bool   `json:"hasToken"`
	Clusters   int    `json:"clusters,omitempty"`
	Reachable  *bool  `json:"reachable,omitempty"`
	Error      string `json:"error,omitempty"`
	// Source describes where the active settings came from: "flags" (CLI
	// flags or environment, not persisted), "file" (saved via the UI,
	// persisted), or empty when not configured.
	Source  string `json:"source,omitempty"`
	Persist bool   `json:"persist"`

	// KubeconfigDir is the active kubeconfig directory ("" when kui uses
	// the default kubeconfig file / $KUBECONFIG).
	KubeconfigDir string `json:"kubeconfigDir,omitempty"`

	// Servers lists every connected Rancher management server. With one
	// server the legacy top-level fields (URL/HasToken/Clusters/…) mirror
	// it, so older clients keep working.
	Servers []rancherServerStatus `json:"servers,omitempty"`
}

// rancherServerStatus is one connected server's view for the settings
// dialog. The token is never included — only whether one is saved.
type rancherServerStatus struct {
	URL      string `json:"url"`
	ID       string `json:"id"`
	HasToken bool   `json:"hasToken"`
	Clusters int    `json:"clusters,omitempty"`
	// Reachable is nil when the server has not answered the last status
	// probe; otherwise the probe result.
	Reachable *bool  `json:"reachable,omitempty"`
	Error     string `json:"error,omitempty"`
	// Source is where this server's settings came from: "flags", "file", or
	// "session" (added via the UI but the file write failed).
	Source string `json:"source,omitempty"`
}

const rancherURLBodyLimit = 1 << 10
const rancherTokenBodyLimit = 4 << 10

// initRancher restores every Rancher connection persisted in the settings
// store (plus the kubeconfig directory). CLI flags take precedence: when
// flags configured a Rancher source, the persisted entries are left on disk
// but the flag-configured one is the only active source. The persisted
// kubeconfig directory applies unless a --kubeconfig flag was given.
func (s *Server) initRancher(flagConfigured bool) {
	if s.settingsStore == nil {
		s.settingsStore = settings.DefaultStoreOrNoop()
	}
	s.restoreRanchers(s.settingsStore.Ranchers(), flagConfigured)
	if kc := s.settingsStore.Kubeconfig(); kc != nil && strings.TrimSpace(kc.Dir) != "" && !s.flagKubeconfig {
		if err := s.mgr.SetKubeconfigDir(kc.Dir); err != nil {
			log.Printf("restore kubeconfig directory %q from settings: %v", kc.Dir, err)
		}
	}
}

// restoreRanchers attaches one source per persisted connection (skipping
// ones with unusable URLs) and records their origin. A flag-configured
// Rancher connection replaces the whole set for this process.
func (s *Server) restoreRanchers(persisted []settings.Rancher, flagConfigured bool) {
	if flagConfigured {
		return
	}
	var sources []*k8s.RancherSource
	for _, entry := range persisted {
		source, err := k8s.NewRancherSource(entry.URL, entry.Token, entry.InsecureSkipTLSVerify)
		if err != nil {
			log.Printf("restore Rancher connection %q: %v", entry.URL, err)
			continue
		}
		sources = append(sources, source)
	}
	if len(sources) == 0 {
		return
	}
	s.mgr.SetRancherSources(sources)
	s.rancherMu.Lock()
	s.rancherSourceKind = "file"
	// rancherToken keeps the last restored token so a settings-dialog save
	// for that server can reuse it when the token field is left blank.
	for _, entry := range persisted {
		if entry.Token != "" {
			s.rancherToken = entry.Token
		}
	}
	s.rancherMu.Unlock()
}

func (s *Server) handleRancherStatus(w http.ResponseWriter, _ *http.Request) {
	sources := s.mgr.Ranchers()
	s.rancherMu.RLock()
	kind, hasToken := s.rancherSourceKind, s.rancherToken != ""
	s.rancherMu.RUnlock()
	if len(sources) == 0 {
		writeJSON(w, http.StatusOK, rancherSettingsResponse{})
		return
	}
	resp := rancherSettingsResponse{
		Configured:    true,
		HasToken:      hasToken,
		Source:        kind,
		Persist:       kind == "file",
		KubeconfigDir: s.mgr.KubeconfigDir(),
	}
	// Probe each server concurrently; the status endpoint stays responsive
	// with several slow servers.
	ctx, cancel := context.WithTimeout(context.Background(), rancherHTTPStatusTimeout)
	defer cancel()
	type probe struct {
		id  string
		out map[string]any
		err error
	}
	probes := make([]probe, len(sources))
	var wg sync.WaitGroup
	for i, source := range sources {
		probes[i] = probe{id: source.ID()}
		wg.Add(1)
		go func(i int, source *k8s.RancherSource) {
			defer wg.Done()
			pctx, pcancel := context.WithTimeout(ctx, rancherHTTPStatusTimeout)
			defer pcancel()
			out, err := source.Status(pctx)
			probes[i].out, probes[i].err = out, err
		}(i, source)
	}
	wg.Wait()
	for i := range probes {
		p := &probes[i]
		server := rancherServerStatus{ID: p.id, Source: kind, HasToken: kind != ""}
		if p.out != nil {
			server.URL, _ = p.out["url"].(string)
			// Live cluster count when the probe succeeded; the imported
			// count otherwise (what this source is currently serving).
			if count, ok := p.out["available"].(int); ok {
				server.Clusters = count
			} else if count, ok := p.out["clusters"].(int); ok {
				server.Clusters = count
			}
			if reachable, ok := p.out["reachable"].(bool); ok {
				server.Reachable = &reachable
			}
		}
		if p.err != nil {
			server.Error = p.err.Error()
		}
		resp.Servers = append(resp.Servers, server)
	}
	// Single-server compatibility: mirror the one connection into the legacy
	// top-level fields.
	if len(resp.Servers) == 1 {
		only := resp.Servers[0]
		resp.URL = only.URL
		resp.Clusters = only.Clusters
		resp.Reachable = only.Reachable
		resp.Error = only.Error
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRancherSave(w http.ResponseWriter, r *http.Request) {
	var req rancherSettingsRequest
	if !decodeJSONBody(w, r, rancherURLBodyLimit+rancherTokenBodyLimit, &req) {
		http.Error(w, "invalid Rancher settings payload", http.StatusBadRequest)
		return
	}
	// Kubeconfig directory: only changed when the field was sent (nil means
	// the dialog did not touch it — e.g. an older client). Applied before the
	// Rancher work so a bad dir fails fast without touching Rancher state.
	if req.KubeconfigDir != nil {
		dir := strings.TrimSpace(*req.KubeconfigDir)
		if dir != "" {
			if err := s.mgr.SetKubeconfigDir(dir); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			if err := s.mgr.SetKubeconfigDir(""); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if err := s.settingsStore.SaveKubeconfig(&settings.Kubeconfig{Dir: dir}); err != nil {
			http.Error(w, "kubeconfig directory applied, but saving the settings file failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		// Rancher section untouched. Valid only when the payload carried a
		// kubeconfigDir field (the dialog always sends it); an older client
		// sending neither URL nor dir is still an error.
		if req.KubeconfigDir == nil {
			http.Error(w, "Rancher server URL is required", http.StatusBadRequest)
			return
		}
		// Rancher section untouched: keep the current Rancher state; the
		// kubeconfig directory (applied above) is already persisted.
		resp := s.rancherStateResponse()
		resp.KubeconfigDir = s.mgr.KubeconfigDir()
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if strings.TrimSpace(req.Disconnect) != "" {
		http.Error(w, "connect and disconnect are separate actions", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		// Keep the matching saved token when the field is left blank: the
		// token for THIS server, not whichever server connected last.
		token = s.savedTokenFor(url)
	}
	if strings.ContainsAny(token, "\n\r") {
		http.Error(w, "Rancher API token must be a single line", http.StatusBadRequest)
		return
	}
	source, err := k8s.NewRancherSource(url, token, req.InsecureSkipTLSVerify)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Verify the endpoint and token before switching the active source, so a
	// typo cannot silently empty the context switcher.
	ctx, cancel := context.WithTimeout(context.Background(), rancherHTTPStatusTimeout)
	defer cancel()
	status, probeErr := source.Status(ctx)
	if probeErr != nil {
		msg := probeErr.Error()
		if status["url"] != nil {
			msg = "Rancher is unreachable with these settings: " + msg
		}
		http.Error(w, msg, http.StatusBadGateway)
		return
	}
	// Persist before activating: a settings file that fails to write must
	// not leave the UI claiming a saved connection.
	if err := s.settingsStore.SaveRancher(&settings.Rancher{
		URL:                   url,
		Token:                 token,
		InsecureSkipTLSVerify: req.InsecureSkipTLSVerify,
	}); err != nil {
		http.Error(w, "Rancher connected, but saving the settings file failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.attachSource(source, token)
	resp := rancherSettingsResponse{
		Configured: true,
		URL:        url,
		HasToken:   token != "",
		Reachable:  boolPtr(true),
		Persist:    true,
		Source:     "file",
	}
	if count, ok := status["available"].(int); ok {
		resp.Clusters = count
	}
	resp.Servers = s.serverStatuses()
	writeJSON(w, http.StatusOK, resp)
}

// rancherStateResponse builds the untouched-Rancher-state response for the
// settings dialog.
func (s *Server) rancherStateResponse() rancherSettingsResponse {
	sources := s.mgr.Ranchers()
	s.rancherMu.RLock()
	kind, hasToken := s.rancherSourceKind, s.rancherToken != ""
	s.rancherMu.RUnlock()
	resp := rancherSettingsResponse{
		Configured: len(sources) > 0,
		HasToken:   hasToken,
		Source:     kind,
		Persist:    kind == "file",
	}
	if len(sources) == 1 {
		resp.URL = firstURL(sources[0])
	}
	return resp
}

// serverStatuses probes every connected server for the dialog's list.
func (s *Server) serverStatuses() []rancherServerStatus {
	sources := s.mgr.Ranchers()
	if len(sources) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), rancherHTTPStatusTimeout)
	defer cancel()
	out := make([]rancherServerStatus, 0, len(sources))
	for _, source := range sources {
		server := rancherServerStatus{ID: source.ID(), Source: s.activeSourceKind(), HasToken: s.hasToken()}
		pctx, pcancel := context.WithTimeout(ctx, rancherHTTPStatusTimeout)
		st, err := source.Status(pctx)
		pcancel()
		if st != nil {
			server.URL, _ = st["url"].(string)
			if count, ok := st["clusters"].(int); ok {
				server.Clusters = count
			}
			if reachable, ok := st["reachable"].(bool); ok {
				server.Reachable = &reachable
			}
		}
		if err != nil {
			server.Error = err.Error()
		}
		out = append(out, server)
	}
	return out
}

// firstURL reads a source's server URL from a cheap status probe.
func firstURL(source *k8s.RancherSource) string {
	ctx, cancel := context.WithTimeout(context.Background(), rancherHTTPStatusTimeout)
	defer cancel()
	st, err := source.Status(ctx)
	if err != nil && st == nil {
		return ""
	}
	url, _ := st["url"].(string)
	return url
}

// savedTokenFor returns the persisted token for the given server URL, or the
// in-memory fallback when nothing is persisted for it.
func (s *Server) savedTokenFor(url string) string {
	for _, entry := range s.settingsStore.Ranchers() {
		if strings.EqualFold(entry.URL, url) {
			return entry.Token
		}
	}
	s.rancherMu.RLock()
	defer s.rancherMu.RUnlock()
	return s.rancherToken
}

// attachSource adds one source to the active set (keeping the others) and
// records the origin marker.
func (s *Server) attachSource(source *k8s.RancherSource, token string) {
	s.rancherMu.Lock()
	s.rancherToken = token
	s.rancherSourceKind = "file"
	s.rancherMu.Unlock()
	sources := s.mgr.Ranchers()
	for i, existing := range sources {
		if existing.ID() == source.ID() {
			sources[i] = source // replace the same server's source
			s.mgr.SetRancherSources(sources)
			return
		}
	}
	s.mgr.SetRancherSources(append(sources, source))
}

func (s *Server) handleRancherRemove(w http.ResponseWriter, r *http.Request) {
	var req rancherSettingsRequest
	target := ""
	if decodeJSONBody(w, r, rancherURLBodyLimit, &req) {
		target = strings.TrimSpace(req.Disconnect)
		if target == "" {
			target = strings.TrimSpace(req.URL)
		}
	}
	// Disconnect now, then clear the persisted entries. A failed file write
	// still disconnects, and the error is reported so the user knows the
	// stale entry will reconnect on next start.
	if err := s.settingsStore.ClearRancher(target); err != nil {
		s.detachSources(target)
		http.Error(w, "Rancher disconnected, but removing the saved settings failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.detachSources(target)
	resp := rancherSettingsResponse{}
	if sources := s.mgr.Ranchers(); len(sources) > 0 {
		kind := s.activeSourceKind()
		resp.Configured = true
		resp.Source = kind
		resp.Persist = kind == "file"
		resp.HasToken = s.hasToken()
		if len(sources) == 1 {
			resp.URL = firstURL(sources[0])
		}
		resp.Servers = s.serverStatuses()
	}
	writeJSON(w, http.StatusOK, resp)
}

// detachSources removes one server (by URL, empty = all) from the active
// set, then clears the origin marker when nothing is left.
func (s *Server) detachSources(url string) {
	if strings.TrimSpace(url) == "" {
		s.setRancherSource(nil, "", "")
		return
	}
	id := k8s.RancherSourceID(url)
	var kept []*k8s.RancherSource
	for _, source := range s.mgr.Ranchers() {
		if source.ID() != id {
			kept = append(kept, source)
		}
	}
	s.mgr.SetRancherSources(kept)
	if len(kept) == 0 {
		s.rancherMu.Lock()
		s.rancherSourceKind = ""
		s.rancherToken = ""
		s.rancherMu.Unlock()
	}
}

// activeSourceKind returns where the active Rancher settings came from.
func (s *Server) activeSourceKind() string {
	s.rancherMu.RLock()
	defer s.rancherMu.RUnlock()
	return s.rancherSourceKind
}

// hasToken reports whether a token is set for the active source.
func (s *Server) hasToken() bool {
	s.rancherMu.RLock()
	defer s.rancherMu.RUnlock()
	return s.rancherToken != ""
}

// setRancherSource swaps the active Rancher source, in-memory token, and
// settings origin marker.
func (s *Server) setRancherSource(source *k8s.RancherSource, token, kind string) {
	s.rancherMu.Lock()
	s.rancherToken = token
	s.rancherSourceKind = kind
	s.rancherMu.Unlock()
	s.mgr.SetRancher(source)
}

// boolPtr is a small helper for optional JSON booleans.
func boolPtr(v bool) *bool { return &v }

const rancherHTTPStatusTimeout = 20 * time.Second
