package web

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rusik69/kui/internal/k8s"
)

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var templateBufferPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// Server holds the shared state for every HTTP handler.
type Server struct {
	mgr              *k8s.Manager
	forwards         *k8s.ForwardManager
	tmpl             *template.Template
	mux              *http.ServeMux
	csrfToken        string
	debugImage       string
	defaultContext   string
	defaultNamespace string
	rancherMu        sync.Mutex
	rancherToken     string // in-memory only; write-only from the UI's perspective
}

// New builds a Server with routes registered and templates parsed.
func New(mgr *k8s.Manager) (*Server, error) {
	funcs := template.FuncMap{
		"join": func(sep string, items []string) string {
			out := ""
			for i, s := range items {
				if i > 0 {
					out += sep
				}
				out += s
			}
			return out
		},
		"cellClass": func(classes []string, i int) string {
			if i >= 0 && i < len(classes) {
				return classes[i]
			}
			return ""
		},
		"title": func(s string) string {
			if s == "" {
				return s
			}
			r := []rune(s)
			r[0] = unicode.ToUpper(r[0])
			return string(r)
		},
		"metricKey": func(namespace, name string) string {
			return strings.TrimPrefix(namespace+"/"+name, "/")
		},
		"hasString": func(values []string, target string) bool {
			for _, value := range values {
				if value == target {
					return true
				}
			}
			return false
		},
		"groupIcon": GroupIcon,
		"pct": func(used, total int64) int {
			if total <= 0 {
				return 0
			}
			p := used * 100 / total
			if p > 100 {
				p = 100
			}
			return int(p)
		},
		"cpuFmt": func(milli int64) string {
			if milli >= 1000 {
				return fmt.Sprintf("%.1f", float64(milli)/1000)
			}
			return fmt.Sprintf("%dm", milli)
		},
		"memFmt": func(b int64) string {
			const unit = 1024
			if b < unit {
				return fmt.Sprintf("%dB", b)
			}
			div, exp := int64(unit), 0
			for n := b / unit; n >= unit; n /= unit {
				div *= unit
				exp++
			}
			return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
		},
	}
	tmpl, err := template.New("kui").Funcs(funcs).ParseFS(templateFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, err
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("create CSRF token: %w", err)
	}
	csrfToken := base64.RawURLEncoding.EncodeToString(tokenBytes)

	s := &Server{
		mgr:        mgr,
		forwards:   k8s.NewForwardManager(),
		tmpl:       tmpl,
		mux:        http.NewServeMux(),
		csrfToken:  csrfToken,
		debugImage: k8s.DebugPodShellImage,
	}
	s.routes()
	return s, nil
}

// SetDebugImage configures the container image used for ephemeral debug shells.
func (s *Server) SetDebugImage(image string) {
	if image != "" {
		s.debugImage = image
	}
}

// SetDefaults configures the context and namespace used for an initial visit.
func (s *Server) SetDefaults(contextName, namespace string) {
	s.defaultContext = contextName
	s.defaultNamespace = namespace
}

// SetRancherToken records the Rancher API token supplied at startup so the
// settings dialog can keep it when saving a new URL without retyping it.
// The token stays in process memory only.
func (s *Server) SetRancherToken(token string) {
	s.rancherMu.Lock()
	s.rancherToken = token
	s.rancherMu.Unlock()
}

func (s *Server) Close() {
	if s.forwards != nil {
		s.forwards.StopAll()
	}
}

const apiRequestTimeout = 30 * time.Second

func (s *Server) Handler() http.Handler {
	return withAPIRequestTimeout(apiRequestTimeout, s.mux)
}

// WithAccessLog logs request metadata without including query parameters.
func WithAccessLog(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.Printf("%s %s %d %s", r.Method, r.URL.EscapedPath(), recorder.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(p []byte) (int, error) {
	return w.ResponseWriter.Write(p)
}

func (w *statusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	w.status = http.StatusSwitchingProtocols
	return conn, rw, nil
}

func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func withAPIRequestTimeout(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLongLivedRequest(r) || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isLongLivedRequest(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	path := r.URL.Path
	return strings.HasSuffix(path, "/watch") || strings.HasSuffix(path, "/logs") || strings.HasSuffix(path, "/exec")
}

func (s *Server) protectMutation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOriginRequest(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Kui-CSRF")), []byte(s.csrfToken)) != 1 {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func sameOriginRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(u.Scheme, scheme) && strings.EqualFold(u.Host, r.Host)
}

func (s *Server) routes() {
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	// Context-scoped routes live under /c/ so they can never collide with
	// /static/ in Go's ServeMux pattern-overlap check (a bare /{ctx}/...
	// wildcard is ambiguous with a literal /static/ prefix).
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	s.mux.HandleFunc("GET /readyz", s.handleReady)

	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /c/{ctx}", s.handleContextPage)

	s.mux.HandleFunc("GET /c/{ctx}/{ns}/{resource}", s.handleResourceList)
	s.mux.HandleFunc("GET /c/{ctx}/{ns}/{resource}/{name}", s.handleResourceDetail)
	s.mux.HandleFunc("PUT /c/{ctx}/{ns}/{resource}/{name}/yaml", s.protectMutation(s.handleYAMLApply))
	s.mux.HandleFunc("PUT /c/{ctx}/{ns}/{resource}/{name}/yaml/diff", s.protectMutation(s.handleYAMLDiff))
	s.mux.HandleFunc("DELETE /c/{ctx}/{ns}/{resource}/{name}", s.protectMutation(s.handleResourceDelete))
	s.mux.HandleFunc("GET /c/{ctx}/{ns}/{resource}/watch", s.handleWatch)
	s.mux.HandleFunc("POST /c/{ctx}/bulk-delete", s.protectMutation(s.handleBulkDelete))

	s.mux.HandleFunc("GET /c/{ctx}/{ns}/pods/{name}/logs", s.handleLogs)
	s.mux.HandleFunc("GET /c/{ctx}/{ns}/pods/{name}/exec", s.handleExec)
	s.mux.HandleFunc("POST /c/{ctx}/{ns}/pods/{name}/portforward", s.protectMutation(s.handlePortForwardStart))
	s.mux.HandleFunc("GET /portforward", s.handlePortForwardList)
	s.mux.HandleFunc("DELETE /portforward/{id}", s.protectMutation(s.handlePortForwardStop))

	s.mux.HandleFunc("POST /c/{ctx}/{ns}/debug-shell", s.protectMutation(s.handleDebugShell))
	s.mux.HandleFunc("POST /c/{ctx}/{ns}/debug-shell/{pod}/kill", s.protectMutation(s.handleDebugShellKill))
	s.mux.HandleFunc("POST /c/{ctx}/{ns}/nodes/{node}/debug-shell", s.protectMutation(s.handleNodeDebugShell))

	s.mux.HandleFunc("GET /c/{ctx}/metrics/pods", s.handleMetricsPods)
	s.mux.HandleFunc("GET /c/{ctx}/metrics/nodes", s.handleMetricsNodes)
	s.mux.HandleFunc("GET /c/{ctx}/status", s.handleStatus)
	s.mux.HandleFunc("GET /c/{ctx}/counts", s.handleCounts)
	s.mux.HandleFunc("GET /c/{ctx}/search", s.handleSearch)
	s.mux.HandleFunc("POST /c/{ctx}/bulk-metadata", s.protectMutation(s.handleBulkMetadata))
	s.mux.HandleFunc("POST /c/{ctx}/bulk-yaml", s.protectMutation(s.handleBulkYAMLExport))
	s.mux.HandleFunc("POST /c/{ctx}/{ns}/deployments/{name}/scale", s.protectMutation(s.handleDeploymentScale))
	s.mux.HandleFunc("POST /c/{ctx}/{ns}/deployments/{name}/action", s.protectMutation(s.handleDeploymentAction))
	s.mux.HandleFunc("GET /c/{ctx}/{ns}/deployments/{name}/status", s.handleDeploymentStatus)
	s.mux.HandleFunc("GET /c/{ctx}/{ns}/deployments/{name}/history", s.handleDeploymentHistory)

	// Rancher setup is global (not per-context): the imported clusters appear
	// in every context's switcher. Mutations carry the CSRF token.
	s.mux.HandleFunc("GET /rancher", s.handleRancherStatus)
	s.mux.HandleFunc("PUT /rancher", s.protectMutation(s.handleRancherSave))
	s.mux.HandleFunc("DELETE /rancher", s.protectMutation(s.handleRancherRemove))
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	buf := templateBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		if buf.Cap() <= 1<<20 {
			templateBufferPool.Put(buf)
		}
	}()
	if err := s.tmpl.ExecuteTemplate(buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = buf.WriteTo(w)
}
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	contexts, err := s.mgr.ListContexts()
	if err != nil {
		http.Error(w, fmt.Sprintf("kubeconfig unavailable: %v", err), http.StatusServiceUnavailable)
		return
	}
	selected := s.defaultContext
	if selected == "" {
		selected, err = s.mgr.DefaultContext()
		if err != nil {
			http.Error(w, fmt.Sprintf("kubeconfig unavailable: %v", err), http.StatusServiceUnavailable)
			return
		}
	}
	if selected == "" {
		http.Error(w, "no current context configured", http.StatusServiceUnavailable)
		return
	}
	for _, ctx := range contexts {
		if ctx.Name == selected {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "ready: %s\n", selected)
			return
		}
	}
	http.Error(w, fmt.Sprintf("context %q not found in kubeconfig", selected), http.StatusServiceUnavailable)
}
