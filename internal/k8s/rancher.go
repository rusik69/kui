package k8s

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// RancherContextPrefix namespaces contexts imported from a Rancher management
// server. Every Rancher cluster becomes a context named
// "rancher/<cluster name>", which can never collide with a file-based
// kubeconfig context unless that file deliberately uses the same prefix.
const RancherContextPrefix = "rancher/"

const (
	rancherClusterListPath = "/v3/clusters"
	// Rancher caps a single collection page at 10000 items; limit=-1 asks for
	// that maximum instead of the norman default of 1000.
	rancherHTTPTimeout         = 15 * time.Second
	rancherRebuildTTL          = 5 * time.Minute
	rancherKubeconfigTTL       = time.Hour
	rancherListBodyLimit       = 16 << 20
	rancherKubeconfigBodyLimit = 4 << 20
	rancherErrorSnippetLimit   = 200
)

// RancherSource imports cluster kubeconfigs from a Rancher management server.
// It lists the clusters the API token can access and asks Rancher to generate
// a kubeconfig for each one; every imported cluster surfaces as a context
// named "rancher/<cluster name>" that behaves like any kubeconfig context.
// Credentials stay in memory — nothing is written to the kubeconfig file.
type RancherSource struct {
	base  string // normalized server URL without a trailing slash
	token string
	http  *http.Client

	// TTLs are fields so tests can shorten them.
	rebuildTTL    time.Duration
	kubeconfigTTL time.Duration

	mu          sync.Mutex
	built       *clientcmdapi.Config
	builtAt     time.Time
	fingerprint string
	kubeconfigs map[string]rancherKubeconfigCache
}

type rancherCluster struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Removed string `json:"removed"`
}

type rancherClusterList struct {
	Data []rancherCluster `json:"data"`
}

type rancherKubeconfigOutput struct {
	Config string `json:"config"`
}

type rancherKubeconfigCache struct {
	cfg *clientcmdapi.Config
	raw string
	at  time.Time
}

// NewRancherSource validates the Rancher server URL and returns a source that
// authenticates with the given API bearer token.
func NewRancherSource(rawURL, token string, insecureSkipTLSVerify bool) (*RancherSource, error) {
	trimmed := strings.TrimSpace(rawURL)
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse Rancher URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("Rancher URL %q must use http or https", rawURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("Rancher URL %q is missing a host", rawURL)
	}
	if u.User != nil {
		return nil, fmt.Errorf("Rancher URL %q must not embed credentials; pass the API token separately", rawURL)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecureSkipTLSVerify {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
	}
	return &RancherSource{
		base:          strings.TrimSuffix(u.String(), "/"),
		token:         strings.TrimSpace(token),
		http:          &http.Client{Timeout: rancherHTTPTimeout, Transport: transport},
		rebuildTTL:    rancherRebuildTTL,
		kubeconfigTTL: rancherKubeconfigTTL,
		kubeconfigs:   map[string]rancherKubeconfigCache{},
	}, nil
}

// Contexts lists every Rancher cluster the token can access as a context.
// The names are prefixed with RancherContextPrefix; none is ever reported as
// the current context — Rancher imports have no file-backed current-context.
func (s *RancherSource) Contexts(ctx context.Context) ([]ContextInfo, error) {
	cfg, err := s.mergedConfig(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ContextInfo, 0, len(cfg.Contexts))
	for name, c := range cfg.Contexts {
		out = append(out, ContextInfo{Name: name, Cluster: c.Cluster, Namespace: c.Namespace})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// RestConfig builds the REST configuration for a context previously returned
// by Contexts. The merged Rancher view is treated as read-only by callers.
func (s *RancherSource) RestConfig(ctx context.Context, ctxName string) (*rest.Config, error) {
	if !strings.HasPrefix(ctxName, RancherContextPrefix) {
		return nil, fmt.Errorf("context %q did not come from Rancher", ctxName)
	}
	cfg, err := s.mergedConfig(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := cfg.Contexts[ctxName]; !ok {
		return nil, fmt.Errorf("rancher context %q is not available", ctxName)
	}
	clientCfg := clientcmd.NewNonInteractiveClientConfig(*cfg, ctxName, &clientcmd.ConfigOverrides{}, nil)
	restCfg, err := clientCfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build client config for rancher context %q: %w", ctxName, err)
	}
	return restCfg, nil
}

// Fingerprint identifies the current Rancher context set. It changes whenever
// the cluster list or any generated kubeconfig changes, letting the Manager
// drop clients built from stale credentials.
func (s *RancherSource) Fingerprint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fingerprint
}

// mergedConfig returns the kubeconfig view containing one context per
// accessible Rancher cluster, rebuilding it when the cached copy is older
// than rebuildTTL. Rebuilds serialize behind the source mutex; the steady
// state costs one cluster-list request because generated kubeconfigs are
// reused for kubeconfigTTL.
func (s *RancherSource) mergedConfig(ctx context.Context) (*clientcmdapi.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.built != nil && time.Since(s.builtAt) < s.rebuildTTL {
		return s.built, nil
	}
	cfg, err := s.rebuildLocked(ctx)
	if err != nil {
		if s.built != nil {
			// Serve the last good cluster set while Rancher is unreachable;
			// a stale context list beats no context list.
			return s.built, nil
		}
		return nil, err
	}
	s.built, s.builtAt = cfg, time.Now()
	return cfg, nil
}

func (s *RancherSource) rebuildLocked(ctx context.Context) (*clientcmdapi.Config, error) {
	clusters, err := s.listClustersLocked(ctx)
	if err != nil {
		return nil, err
	}
	merged := clientcmdapi.NewConfig()
	usedNames := map[string]bool{}
	parts := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		if cluster.Removed != "" {
			continue
		}
		gen, raw, err := s.kubeconfigForLocked(ctx, cluster)
		if err != nil {
			// Clusters that cannot produce a kubeconfig yet (still
			// provisioning, agent offline, permission denied) are skipped
			// until the next refresh instead of failing the whole import.
			continue
		}
		name := RancherContextPrefix + clusterDisplayName(cluster)
		if usedNames[name] {
			// Two clusters sharing a display name: fall back to the unique
			// cluster ID for the second one.
			name = RancherContextPrefix + cluster.ID
		}
		usedNames[name] = true
		if err := mergeGeneratedKubeconfig(merged, gen, name); err != nil {
			continue
		}
		parts = append(parts, cluster.ID+"\x00"+name+"\x00"+kubeconfigHash(raw))
	}
	sort.Strings(parts)
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	s.fingerprint = hex.EncodeToString(digest[:])
	return merged, nil
}

// kubeconfigForLocked returns the parsed generated kubeconfig for a cluster,
// regenerating it when the cached copy is older than kubeconfigTTL. If
// regeneration fails, the previous kubeconfig is served until Rancher
// recovers; a stale credential beats no credential.
func (s *RancherSource) kubeconfigForLocked(ctx context.Context, cluster rancherCluster) (*clientcmdapi.Config, string, error) {
	if cached, ok := s.kubeconfigs[cluster.ID]; ok && time.Since(cached.at) < s.kubeconfigTTL {
		return cached.cfg, cached.raw, nil
	}
	gen, raw, err := s.generateKubeconfig(ctx, cluster)
	if err != nil {
		if cached, ok := s.kubeconfigs[cluster.ID]; ok {
			return cached.cfg, cached.raw, nil
		}
		return nil, "", err
	}
	s.kubeconfigs[cluster.ID] = rancherKubeconfigCache{cfg: gen, raw: raw, at: time.Now()}
	return gen, raw, nil
}

func (s *RancherSource) listClustersLocked(ctx context.Context) ([]rancherCluster, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+rancherClusterListPath+"?limit=-1", nil)
	if err != nil {
		return nil, fmt.Errorf("build Rancher cluster list request: %w", err)
	}
	s.setAuth(req)
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list Rancher clusters: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, rancherListBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("read Rancher cluster list: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list Rancher clusters: %s: %s", resp.Status, rancherErrorSnippet(body))
	}
	var list rancherClusterList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decode Rancher cluster list: %w", err)
	}
	return list.Data, nil
}

func (s *RancherSource) generateKubeconfig(ctx context.Context, cluster rancherCluster) (*clientcmdapi.Config, string, error) {
	endpoint := fmt.Sprintf("%s/v3/clusters/%s?action=generateKubeconfig", s.base, url.PathEscape(cluster.ID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, "", fmt.Errorf("build Rancher kubeconfig request: %w", err)
	}
	s.setAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("generate kubeconfig for Rancher cluster %q: %w", cluster.ID, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, rancherKubeconfigBodyLimit))
	if err != nil {
		return nil, "", fmt.Errorf("read generated kubeconfig for Rancher cluster %q: %w", cluster.ID, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("generate kubeconfig for Rancher cluster %q: %s: %s", cluster.ID, resp.Status, rancherErrorSnippet(body))
	}
	var out rancherKubeconfigOutput
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, "", fmt.Errorf("decode generated kubeconfig for Rancher cluster %q: %w", cluster.ID, err)
	}
	if strings.TrimSpace(out.Config) == "" {
		return nil, "", fmt.Errorf("Rancher returned an empty kubeconfig for cluster %q", cluster.ID)
	}
	gen, err := clientcmd.Load([]byte(out.Config))
	if err != nil {
		return nil, "", fmt.Errorf("parse generated kubeconfig for Rancher cluster %q: %w", cluster.ID, err)
	}
	return gen, out.Config, nil
}

func (s *RancherSource) setAuth(req *http.Request) {
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
}

func clusterDisplayName(cluster rancherCluster) string {
	if cluster.Name != "" {
		return cluster.Name
	}
	return cluster.ID
}

// mergeGeneratedKubeconfig copies the context Rancher generated (plus the
// cluster and user entries it references) into dst under a namespaced
// context name. Cluster and user entries are re-keyed per context: Rancher
// names them after the cluster, so two clusters sharing a display name would
// otherwise overwrite each other's credentials.
func mergeGeneratedKubeconfig(dst *clientcmdapi.Config, gen *clientcmdapi.Config, ctxName string) error {
	srcName := gen.CurrentContext
	if gen.Contexts[srcName] == nil {
		srcName = ""
		for name := range gen.Contexts {
			if srcName == "" || name < srcName {
				srcName = name
			}
		}
	}
	if srcName == "" {
		return fmt.Errorf("generated kubeconfig for %q has no contexts", ctxName)
	}
	src := *gen.Contexts[srcName]
	if cluster, ok := gen.Clusters[src.Cluster]; ok {
		key := ctxName + ":cluster"
		dst.Clusters[key] = cluster
		src.Cluster = key
	}
	if user, ok := gen.AuthInfos[src.AuthInfo]; ok {
		key := ctxName + ":user"
		dst.AuthInfos[key] = user
		src.AuthInfo = key
	}
	dst.Contexts[ctxName] = &src
	return nil
}

func kubeconfigHash(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func rancherErrorSnippet(body []byte) string {
	snippet := strings.TrimSpace(string(body))
	if snippet == "" {
		return "empty response"
	}
	if len(snippet) > rancherErrorSnippetLimit {
		snippet = snippet[:rancherErrorSnippetLimit] + "…"
	}
	return snippet
}
