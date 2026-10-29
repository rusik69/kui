package web

import (
	"context"
	"log"
	"net/http"
	"strings"
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
}

const rancherTokenEnvVar = "KUI_RANCHER_TOKEN"
const rancherURLBodyLimit = 1 << 10
const rancherTokenBodyLimit = 4 << 10

// initRancher restores Rancher and kubeconfig-directory settings persisted in
// the settings store. CLI flags take precedence: when flags configured a
// Rancher source, the persisted Rancher entry is left untouched on disk but
// not activated. The persisted kubeconfig directory applies unless a
// --kubeconfig flag was given.
func (s *Server) initRancher(flagConfigured bool) {
	if s.settingsStore == nil {
		s.settingsStore = settings.DefaultStoreOrNoop()
	}
	persisted := s.settingsStore.Rancher()
	if persisted != nil && !flagConfigured {
		if source, err := k8s.NewRancherSource(persisted.URL, persisted.Token, persisted.InsecureSkipTLSVerify); err == nil {
			s.mgr.SetRancher(source)
			s.rancherMu.Lock()
			s.rancherToken = persisted.Token
			s.rancherSourceKind = "file"
			s.rancherMu.Unlock()
		}
	}
	if kc := s.settingsStore.Kubeconfig(); kc != nil && strings.TrimSpace(kc.Dir) != "" && !s.flagKubeconfig {
		if err := s.mgr.SetKubeconfigDir(kc.Dir); err != nil {
			log.Printf("restore kubeconfig directory %q from settings: %v", kc.Dir, err)
		}
	}
}

func (s *Server) handleRancherStatus(w http.ResponseWriter, _ *http.Request) {
	source := s.mgr.Rancher()
	if source == nil {
		writeJSON(w, http.StatusOK, rancherSettingsResponse{})
		return
	}
	s.rancherMu.RLock()
	kind, hasToken := s.rancherSourceKind, s.rancherToken != ""
	s.rancherMu.RUnlock()
	resp := rancherSettingsResponse{
		Configured: true,
		HasToken:   hasToken,
		Source:     kind,
		Persist:    kind == "file",
	}
	ctx, cancel := context.WithTimeout(context.Background(), rancherHTTPStatusTimeout)
	defer cancel()
	out, err := source.Status(ctx)
	resp.URL, _ = out["url"].(string)
	if count, ok := out["clusters"].(int); ok {
		resp.Clusters = count
	}
	if reachable, ok := out["reachable"].(bool); ok {
		resp.Reachable = &reachable
	}
	if err != nil {
		resp.Error = err.Error()
	}
	resp.KubeconfigDir = s.mgr.KubeconfigDir()
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
		resp := rancherSettingsResponse{
			Configured:    s.mgr.Rancher() != nil,
			HasToken:      s.hasToken(),
			Source:        s.activeSourceKind(),
			Persist:       s.activeSourceKind() == "file",
			KubeconfigDir: s.mgr.KubeconfigDir(),
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		// Keep the active token when the field is left blank.
		s.rancherMu.RLock()
		token = s.rancherToken
		s.rancherMu.RUnlock()
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
	s.setRancherSource(source, token, "file")
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
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRancherRemove(w http.ResponseWriter, _ *http.Request) {
	// Disconnect now, then clear the persisted entry. A failed file write
	// still disconnects, and the error is reported so the user knows the
	// stale entry will reconnect on next start.
	if err := s.settingsStore.ClearRancher(); err != nil {
		s.setRancherSource(nil, "", "")
		http.Error(w, "Rancher disconnected, but removing the saved settings failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.setRancherSource(nil, "", "")
	writeJSON(w, http.StatusOK, rancherSettingsResponse{})
}

// activeToken returns the in-memory token for the active source.
func (s *Server) activeToken() string {
	s.rancherMu.RLock()
	defer s.rancherMu.RUnlock()
	return s.rancherToken
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
