package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/rusik69/kui/internal/k8s"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

// SearchCommand is one command-palette "jump to" entry.
type SearchCommand struct {
	Label   string
	URL     string
	FullNav bool
}

// SearchObject is one live object match from the fan-out search.
type SearchObject struct {
	Kind, Name, Namespace string
	ListURL               string
	KindCount             int
}
type searchQualifier struct {
	Kind, Namespace, Name string
	Text                  string
	Status, Label, Field  string
	ExactName             bool
}

type searchResults struct {
	Commands      []SearchCommand
	Objects       []SearchObject
	Error         string
	Query         string
	KindFilter    string
	QualifierKind string
	QualifierNS   string
	QualifierName string
	Count         int
	Truncated     bool
	InvalidQuery  bool
	Context       string
	Namespace     string
	Suggestions   []string
}

const searchObjectCap = 30
const searchObjectPerKindCap = 5

func parseSearchQuery(query string) (searchQualifier, error) {
	var parsed searchQualifier
	var text []string
	isSpace := func(ch byte) bool { return ch == ' ' || ch == '\t' || ch == '\n' }
	for i := 0; i < len(query); {
		for i < len(query) && isSpace(query[i]) {
			i++
		}
		if i == len(query) {
			break
		}
		start := i
		for i < len(query) && query[i] != ':' && !isSpace(query[i]) {
			i++
		}
		if i == len(query) || query[i] != ':' {
			i = start
			if query[i] == '"' {
				i++
				var value strings.Builder
				for i < len(query) && query[i] != '"' {
					if query[i] == '\\' {
						i++
						if i == len(query) {
							return parsed, fmt.Errorf("unfinished escape in quoted text")
						}
					}
					value.WriteByte(query[i])
					i++
				}
				if i == len(query) {
					return parsed, fmt.Errorf("missing closing quote")
				}
				i++
				if i < len(query) && !isSpace(query[i]) {
					return parsed, fmt.Errorf("quoted values must be followed by a space")
				}
				text = append(text, value.String())
				continue
			}
			for i < len(query) && !isSpace(query[i]) {
				i++
			}
			text = append(text, query[start:i])
			continue
		}
		key := strings.ToLower(query[start:i])
		switch key {
		case "kind", "namespace", "name", "status", "label", "field":
		default:
			return parsed, fmt.Errorf("unknown qualifier %q", key)
		}
		i++
		var value string
		if i < len(query) && query[i] == '"' {
			i++
			var builder strings.Builder
			for i < len(query) && query[i] != '"' {
				if query[i] == '\\' {
					i++
					if i == len(query) {
						return parsed, fmt.Errorf("unfinished escape in %s value", key)
					}
				}
				builder.WriteByte(query[i])
				i++
			}
			if i == len(query) {
				return parsed, fmt.Errorf("missing closing quote for %s", key)
			}
			i++
			if i < len(query) && !isSpace(query[i]) {
				return parsed, fmt.Errorf("quoted %s value must be followed by a space", key)
			}
			value = builder.String()
		} else {
			valueStart := i
			for i < len(query) && !isSpace(query[i]) {
				i++
			}
			value = query[valueStart:i]
		}
		if strings.TrimSpace(value) == "" {
			return parsed, fmt.Errorf("%s: needs a non-empty value", key)
		}
		if key == "name" && strings.HasPrefix(value, "=") {
			parsed.ExactName = true
			value = strings.TrimPrefix(value, "=")
			if value == "" {
				return parsed, fmt.Errorf("name:= needs a non-empty value")
			}
		}
		switch key {
		case "kind":
			parsed.Kind = value
		case "namespace":
			parsed.Namespace = value
		case "name":
			parsed.Name = value
		case "status":
			parsed.Status = strings.ToLower(value)
			switch parsed.Status {
			case "running", "pending", "failed", "succeeded", "unknown", "ready", "not-ready":
			default:
				return parsed, fmt.Errorf("unsupported status %q", value)
			}
		case "label":
			if _, err := labels.Parse(value); err != nil {
				return parsed, fmt.Errorf("invalid label selector: %v", err)
			}
			parsed.Label = value
		case "field":
			selector, err := fields.ParseSelector(value)
			if err != nil {
				return parsed, fmt.Errorf("invalid field selector: %v", err)
			}
			for _, requirement := range selector.Requirements() {
				if requirement.Field != "metadata.name" && requirement.Field != "metadata.namespace" {
					return parsed, fmt.Errorf("unsupported field selector %q; supported fields are metadata.name and metadata.namespace", requirement.Field)
				}
			}
			parsed.Field = value
		}
	}
	parsed.Text = strings.Join(text, " ")
	return parsed, nil
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctxName := r.PathValue("ctx")
	ns := r.URL.Query().Get("ns")
	if ns == "" {
		ns = "_all"
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results := searchResults{Query: q, Context: ctxName, Namespace: ns, KindFilter: r.URL.Query().Get("kindFilter")}
	if q == "" {
		returned := searchResults{Query: "", Context: ctxName, Namespace: ns}
		s.render(w, "searchresults", returned)
		return
	}
	qualifiers, err := parseSearchQuery(q)
	if err != nil {
		results.InvalidQuery = true
		results.Error = "Search syntax: " + err.Error()
		s.render(w, "searchresults", results)
		return
	}
	results.QualifierKind, results.QualifierNS, results.QualifierName = qualifiers.Kind, qualifiers.Namespace, qualifiers.Name
	needle := strings.ToLower(qualifiers.Text)
	results.Commands = s.searchCommands(ctxName, ns, needle)
	if qualifiers.Kind != "" || qualifiers.Namespace != "" || qualifiers.Name != "" || qualifiers.Status != "" || qualifiers.Label != "" || qualifiers.Field != "" {
		results.Commands = nil
	}
	c, err := s.clients(ctxName)
	if err != nil {
		results.Error = "Object search unavailable. Check the selected context and reconnect, then retry."
	} else {
		fanCtx, cancel := context.WithTimeout(r.Context(), fanOutTimeout)
		defer cancel()
		fanout := k8s.FanOutList(fanCtx, c, ns)
		var found int
		results.Objects, found = searchObjectsFilteredScoped(fanout, ctxName, ns, qualifiers)
		results.Count = found
		results.Truncated = found > len(results.Objects)
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
			results.Error = prefix + strings.Join(failures, ", ") + ". Retry the search or narrow it with kind:."
		}
	}
	s.render(w, "searchresults", results)
}

func (s *Server) searchCommands(ctxName, ns, needle string) []SearchCommand {
	var out []SearchCommand
	add := func(label, path string, fullNav bool) {
		if strings.Contains(strings.ToLower(label), needle) {
			out = append(out, SearchCommand{Label: label, URL: path, FullNav: fullNav})
		}
	}
	pathPrefix := "/c/" + url.PathEscape(ctxName) + "/" + url.PathEscape(ns)
	add("Overview", pathPrefix+"/overview", false)
	add("All resources", pathPrefix+"/all", false)
	for _, rk := range k8s.Registry {
		add("Go to "+rk.Kind, pathPrefix+"/"+rk.Name, false)
	}
	if contexts, err := s.mgr.ListContexts(); err == nil {
		for _, rc := range contexts {
			add("Switch to "+rc.Name, "/c/"+encodeCtx(rc.Name), true)
		}
	}
	return out
}

func searchObjects(results []k8s.KindResult, ctxName, needle string) []SearchObject {
	parsed := searchQualifier{Text: needle}
	objects, _ := searchObjectsFiltered(results, ctxName, parsed)
	return objects
}

func searchObjectsFiltered(results []k8s.KindResult, ctxName string, query searchQualifier) ([]SearchObject, int) {
	return searchObjectsFilteredScoped(results, ctxName, "_all", query)
}

func searchObjectsFilteredScoped(results []k8s.KindResult, ctxName, scopeNS string, query searchQualifier) ([]SearchObject, int) {
	var out []SearchObject
	count := 0
	kindCounts := make(map[string]int)
	var labelSelector labels.Selector = labels.Everything()
	if query.Label != "" {
		parsed, err := labels.Parse(query.Label)
		if err != nil {
			return nil, 0
		}
		labelSelector = parsed
	}
	var fieldSelector fields.Selector = fields.Everything()
	if query.Field != "" {
		parsed, err := fields.ParseSelector(query.Field)
		if err != nil {
			return nil, 0
		}
		fieldSelector = parsed
	}
	matches := func(value, filter string) bool {
		return filter == "" || strings.Contains(strings.ToLower(value), strings.ToLower(filter))
	}
	for _, res := range results {
		if res.Err != nil || !matches(res.Kind.Kind, query.Kind) && !matches(res.Kind.Name, query.Kind) {
			continue
		}
		perKind := 0
		for i := range res.List.Items {
			obj := &res.List.Items[i]
			if scopeNS != "_all" && obj.GetNamespace() != scopeNS {
				continue
			}
			if !matches(obj.GetName(), query.Name) || (query.ExactName && obj.GetName() != query.Name) || !matches(obj.GetNamespace(), query.Namespace) || !matches(obj.GetName(), query.Text) {
				continue
			}
			if !labelSelector.Matches(labels.Set(obj.GetLabels())) || !fieldSelector.Matches(fields.Set{"metadata.name": obj.GetName(), "metadata.namespace": obj.GetNamespace()}) {
				continue
			}
			if query.Status != "" && !searchStatusMatches(obj, query.Status) {
				continue
			}
			count++
			kindCounts[res.Kind.Kind]++
			perKind++
			if perKind > searchObjectPerKindCap || len(out) >= searchObjectCap {
				continue
			}
			listURL := "/c/" + url.PathEscape(ctxName) + "/" + url.PathEscape(nsOrDash(obj.GetNamespace())) + "/" + res.Kind.Name
			out = append(out, SearchObject{Kind: res.Kind.Kind, Name: obj.GetName(), Namespace: obj.GetNamespace(), ListURL: listURL})
		}
	}
	for i := range out {
		out[i].KindCount = kindCounts[out[i].Kind]
	}
	return out, count
}
func searchStatusMatches(obj *unstructured.Unstructured, status string) bool {
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	ready := false
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if ok && condition["type"] == "Ready" && condition["status"] == "True" {
			ready = true
		}
	}
	switch status {
	case "ready":
		return ready
	case "not-ready":
		return !ready
	case "running", "pending", "failed", "succeeded", "unknown":
		return strings.EqualFold(phase, status)
	}
	return false
}
