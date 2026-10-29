package web

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rusik69/kui/internal/k8s"
)

// Rancher settings payloads. The API token is write-only: responses report
// whether a token is set, never the token itself.
type rancherSettingsRequest struct {
	URL                   string `json:"url"`
	Token                 string `json:"token"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify"`
}

type rancherSettingsResponse struct {
	Configured bool   `json:"configured"`
	URL        string `json:"url,omitempty"`
	HasToken   bool   `json:"hasToken"`
	Clusters   int    `json:"clusters,omitempty"`
	Reachable  *bool  `json:"reachable,omitempty"`
	Error      string `json:"error,omitempty"`
}

const rancherTokenEnvVar = "KUI_RANCHER_TOKEN"
const rancherURLBodyLimit = 1 << 10
const rancherTokenBodyLimit = 4 << 10

func (s *Server) handleRancherStatus(w http.ResponseWriter, _ *http.Request) {
	source := s.mgr.Rancher()
	if source == nil {
		writeJSON(w, http.StatusOK, rancherSettingsResponse{})
		return
	}
	resp := rancherSettingsResponse{Configured: true, HasToken: s.rancherToken != ""}
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
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRancherSave(w http.ResponseWriter, r *http.Request) {
	var req rancherSettingsRequest
	if !decodeJSONBody(w, r, rancherURLBodyLimit+rancherTokenBodyLimit, &req) {
		http.Error(w, "invalid Rancher settings payload", http.StatusBadRequest)
		return
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		http.Error(w, "Rancher server URL is required", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		// Keep the existing token when the field is left blank.
		token = s.rancherToken
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
	s.setRancherSource(source, token)
	resp := rancherSettingsResponse{
		Configured: true,
		URL:        url,
		HasToken:   token != "",
		Reachable:  boolPtr(true),
	}
	if count, ok := status["available"].(int); ok {
		resp.Clusters = count
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRancherRemove(w http.ResponseWriter, _ *http.Request) {
	s.setRancherSource(nil, "")
	writeJSON(w, http.StatusOK, rancherSettingsResponse{})
}

// setRancherSource swaps the active Rancher source and the in-memory token.
// The token is never written to disk.
func (s *Server) setRancherSource(source *k8s.RancherSource, token string) {
	s.rancherMu.Lock()
	s.rancherToken = token
	s.rancherMu.Unlock()
	s.mgr.SetRancher(source)
}

// initialRancherToken resolves the token for a flag-configured source: the
// flag value, then the environment variable.
func initialRancherToken(flagToken string) string {
	if strings.TrimSpace(flagToken) != "" {
		return strings.TrimSpace(flagToken)
	}
	return strings.TrimSpace(os.Getenv(rancherTokenEnvVar))
}

func boolPtr(v bool) *bool { return &v }

const rancherHTTPStatusTimeout = 20 * time.Second
