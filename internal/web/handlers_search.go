package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/rusik69/kui/internal/k8s"
)

// SearchCommand is one command-palette "jump to" entry — a resource kind, a
// built-in page, or a context switch. FullNav means a plain browser
// navigation (context switches reload the whole shell); otherwise it's an
// hx-get into #content exactly like a sidebar link.
type SearchCommand struct {
	Label   string
	URL     string
	FullNav bool
}

// SearchObject is one live object match from the fan-out search.
type SearchObject struct {
	Kind, Name, Namespace string
	ListURL               string // the resource's list view, not a detail fragment — see handleSearch doc
}

type searchResults struct {
	Commands []SearchCommand
	Objects  []SearchObject
	Error    string
}

const searchObjectCap = 30
const searchObjectPerKindCap = 5

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctxName := r.PathValue("ctx")
	ns := r.URL.Query().Get("ns")
	if ns == "" {
		ns = "_all"
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		fmt.Fprint(w, `<div class="empty-state" role="status">Type to search&hellip;</div>`)
		return
	}
	needle := strings.ToLower(q)

	results := searchResults{Commands: s.searchCommands(ctxName, ns, needle)}

	c, err := s.clients(ctxName)
	if err != nil {
		results.Error = "Object search unavailable: " + err.Error()
	} else {
		fanCtx, cancel := context.WithTimeout(r.Context(), fanOutTimeout)
		defer cancel()
		fanout := k8s.FanOutList(fanCtx, c, ns)
		results.Objects = searchObjects(fanout, ctxName, needle)
		failures := make([]string, 0, len(fanout))
		successes := 0
		for _, result := range fanout {
			if result.Err != nil {
				failures = append(failures, result.Kind.Kind)
			} else {
				successes++
			}
		}
		if len(failures) > 0 {
			prefix := "Some resource kinds could not be searched: "
			if successes == 0 {
				prefix = "Object search unavailable; failed resource kinds: "
			}
			results.Error = prefix + strings.Join(failures, ", ")
		}
	}

	s.render(w, "searchresults", results)
}

func (s *Server) searchCommands(ctxName, ns, needle string) []SearchCommand {
	var out []SearchCommand
	add := func(label, url string, fullNav bool) {
		if strings.Contains(strings.ToLower(label), needle) {
			out = append(out, SearchCommand{Label: label, URL: url, FullNav: fullNav})
		}
	}

	add("Overview", "/c/"+ctxName+"/"+ns+"/overview", false)
	add("All resources", "/c/"+ctxName+"/"+ns+"/all", false)
	for _, rk := range k8s.Registry {
		add("Go to "+rk.Kind, "/c/"+ctxName+"/"+ns+"/"+rk.Name, false)
	}
	if contexts, err := s.mgr.ListContexts(); err == nil {
		for _, rc := range contexts {
			add("Switch to "+rc.Name, "/c/"+encodeCtx(rc.Name), true)
		}
	}
	return out
}

// searchObjects substring-matches object names (case-insensitive) across
// every kind FanOutList returned. Navigates to the resource's list view
// (same URL a sidebar link uses), not a detail fragment: handleResourceDetail
// renders markup meant for #detail-pane, and hitting it as a #content swap
// would break the list/detail split-pane layout entirely.
func searchObjects(results []k8s.KindResult, ctxName, needle string) []SearchObject {
	var out []SearchObject
	for _, res := range results {
		if res.Err != nil {
			continue
		}
		perKind := 0
		for i := range res.List.Items {
			if len(out) >= searchObjectCap {
				return out
			}
			if perKind >= searchObjectPerKindCap {
				break
			}
			obj := &res.List.Items[i]
			if !strings.Contains(strings.ToLower(obj.GetName()), needle) {
				continue
			}
			out = append(out, SearchObject{
				Kind: res.Kind.Kind, Name: obj.GetName(), Namespace: obj.GetNamespace(),
				ListURL: "/c/" + ctxName + "/" + nsOrDash(obj.GetNamespace()) + "/" + res.Kind.Name,
			})
			perKind++
		}
	}
	return out
}
