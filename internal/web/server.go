package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"unicode"

	"github.com/rusik69/kui/internal/k8s"
)

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Server holds the shared state for every HTTP handler.
type Server struct {
	mgr      *k8s.Manager
	forwards *k8s.ForwardManager
	tmpl     *template.Template
	mux      *http.ServeMux
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

	s := &Server{
		mgr:      mgr,
		forwards: k8s.NewForwardManager(),
		tmpl:     tmpl,
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	// Context-scoped routes live under /c/ so they can never collide with
	// /static/ in Go's ServeMux pattern-overlap check (a bare /{ctx}/...
	// wildcard is ambiguous with a literal /static/ prefix).
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /c/{ctx}", s.handleContextPage)

	s.mux.HandleFunc("GET /c/{ctx}/{ns}/{resource}", s.handleResourceList)
	s.mux.HandleFunc("GET /c/{ctx}/{ns}/{resource}/{name}", s.handleResourceDetail)
	s.mux.HandleFunc("PUT /c/{ctx}/{ns}/{resource}/{name}/yaml", s.handleYAMLApply)
	s.mux.HandleFunc("PUT /c/{ctx}/{ns}/{resource}/{name}/yaml/diff", s.handleYAMLDiff)
	s.mux.HandleFunc("DELETE /c/{ctx}/{ns}/{resource}/{name}", s.handleResourceDelete)
	s.mux.HandleFunc("GET /c/{ctx}/{ns}/{resource}/watch", s.handleWatch)
	s.mux.HandleFunc("POST /c/{ctx}/bulk-delete", s.handleBulkDelete)

	s.mux.HandleFunc("GET /c/{ctx}/{ns}/pods/{name}/logs", s.handleLogs)
	s.mux.HandleFunc("GET /c/{ctx}/{ns}/pods/{name}/exec", s.handleExec)
	s.mux.HandleFunc("POST /c/{ctx}/{ns}/pods/{name}/portforward", s.handlePortForwardStart)
	s.mux.HandleFunc("DELETE /portforward/{id}", s.handlePortForwardStop)

	s.mux.HandleFunc("POST /c/{ctx}/{ns}/debug-shell", s.handleDebugShell)

	s.mux.HandleFunc("GET /c/{ctx}/metrics/pods", s.handleMetricsPods)
	s.mux.HandleFunc("GET /c/{ctx}/metrics/nodes", s.handleMetricsNodes)
	s.mux.HandleFunc("GET /c/{ctx}/status", s.handleStatus)
	s.mux.HandleFunc("GET /c/{ctx}/counts", s.handleCounts)
	s.mux.HandleFunc("GET /c/{ctx}/search", s.handleSearch)
	s.mux.HandleFunc("GET /c/{ctx}/{ns}/deployments/{name}/history", s.handleDeploymentHistory)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
