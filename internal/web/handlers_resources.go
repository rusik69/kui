package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"

	"github.com/rusik69/kui/internal/k8s"
)

func nsOrDash(ns string) string {
	if ns == "" {
		return "_"
	}
	return ns
}

// insertBeforeLast inserts items just before the final element of s (used to
// slot CPU/Mem columns in ahead of the trailing Age column).
func insertBeforeLast(s []string, items ...string) []string {
	if len(s) == 0 {
		return append([]string{}, items...)
	}
	last := s[len(s)-1]
	out := make([]string, 0, len(s)+len(items))
	out = append(out, s[:len(s)-1]...)
	out = append(out, items...)
	out = append(out, last)
	return out
}

// resolveKind finds a ResourceKind by its URL name, checking the static
// registry first and falling back to live discovery (covers CRDs and any
// built-in kind we haven't special-cased).
func resolveKind(c *k8s.Clients, name string) (k8s.ResourceKind, bool) {
	if rk, ok := k8s.ByName(name); ok {
		return rk, true
	}
	all, err := k8s.AllAPIResources(c)
	if err != nil {
		return k8s.ResourceKind{}, false
	}
	for _, rk := range all {
		if rk.Name == name {
			return rk, true
		}
	}
	return k8s.ResourceKind{}, false
}

func (s *Server) handleResourceList(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource")

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	if resource == "all" {
		s.renderAllResourcesDirectory(w, r, ctxName, ns, c)
		return
	}
	if resource == "overview" {
		s.renderOverview(w, r, ctxName, ns, c)
		return
	}

	rk, ok := resolveKind(c, resource)
	if !ok {
		http.Error(w, "unknown resource: "+resource, http.StatusNotFound)
		return
	}

	table, err := s.buildTableData(r.Context(), c, rk, ctxName, ns)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	switch {
	case r.URL.Query().Get("frame") == "table":
		s.render(w, "table", table)
	case r.Header.Get("HX-Request") == "true":
		data, err := s.buildPageData(ctxName, ns, resource)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		s.render(w, "content", data)
	default:
		data, err := s.buildPageData(ctxName, ns, resource)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		s.render(w, "page", data)
	}
}

func (s *Server) buildTableData(ctx context.Context, c *k8s.Clients, rk k8s.ResourceKind, ctxName, ns string) (TableData, error) {
	list, err := k8s.List(ctx, c, rk, ns)
	if err != nil {
		return TableData{}, err
	}
	showNS := ns == "_all" && rk.Namespaced

	hasMetrics := rk.Name == "pods" || rk.Name == "nodes"
	columns := Columns(rk.Name)
	if hasMetrics {
		columns = insertBeforeLast(columns, "CPU", "Mem")
	}

	rows := make([]Row, 0, len(list.Items))
	for i := range list.Items {
		obj := &list.Items[i]
		row := RowCells(rk.Name, obj)
		row.DetailURL = fmt.Sprintf("/c/%s/%s/%s/%s", ctxName, nsOrDash(obj.GetNamespace()), rk.Name, obj.GetName())
		if hasMetrics {
			hadClasses := len(row.CellClasses) == len(row.Cells)
			row.Cells = insertBeforeLast(row.Cells, "–", "–")
			if hadClasses {
				row.CellClasses = insertBeforeLast(row.CellClasses, "cell-cpu", "cell-mem")
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	metricsURL := ""
	if rk.Name == "pods" {
		metricsURL = fmt.Sprintf("/c/%s/metrics/pods?ns=%s", ctxName, ns)
	} else if rk.Name == "nodes" {
		metricsURL = fmt.Sprintf("/c/%s/metrics/nodes", ctxName)
	}

	return TableData{
		Kind:          rk.Kind,
		Columns:       columns,
		Rows:          rows,
		ShowNamespace: showNS,
		MetricsURL:    metricsURL,
		Selectable:    true,
		HasMetrics:    hasMetrics,
	}, nil
}

func (s *Server) renderAllResourcesDirectory(w http.ResponseWriter, r *http.Request, ctxName, ns string, c *k8s.Clients) {
	kinds, err := k8s.AllAPIResources(c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	table := TableData{Kind: "All resources", Columns: []string{"Kind", "Namespaced"}, RowsAreKinds: true}
	for _, rk := range kinds {
		table.Rows = append(table.Rows, Row{
			Name:      rk.Kind,
			Cells:     []string{rk.Kind, fmt.Sprintf("%v", rk.Namespaced)},
			DetailURL: fmt.Sprintf("/c/%s/%s/%s", ctxName, ns, rk.Name),
		})
	}

	if r.Header.Get("HX-Request") == "true" && r.URL.Query().Get("frame") != "table" {
		data, err := s.buildPageData(ctxName, ns, "all")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		s.render(w, "content", data)
		return
	}
	if r.URL.Query().Get("frame") == "table" {
		s.render(w, "table", table)
		return
	}
	data, err := s.buildPageData(ctxName, ns, "all")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.render(w, "page", data)
}

func (s *Server) handleResourceDetail(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource"), r.PathValue("name")

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	rk, ok := resolveKind(c, resource)
	if !ok {
		http.Error(w, "unknown resource: "+resource, http.StatusNotFound)
		return
	}
	obj, err := k8s.Get(r.Context(), c, rk, ns, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	detail, err := s.buildDetailData(r.Context(), c, rk, ctxName, ns, obj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.render(w, "detail", detail)
}

func (s *Server) buildDetailData(ctx context.Context, c *k8s.Clients, rk k8s.ResourceKind, ctxName, ns string, obj *unstructured.Unstructured) (DetailData, error) {
	row := RowCells(rk.Name, obj)

	stripped := obj.DeepCopy()
	unstructured.RemoveNestedField(stripped.Object, "metadata", "managedFields")
	yamlBytes, err := yaml.Marshal(stripped.Object)
	if err != nil {
		return DetailData{}, err
	}

	d := DetailData{
		Context: ctxName, Namespace: obj.GetNamespace(), Resource: rk.Name, Name: obj.GetName(), Kind: rk.Kind,
		Age:         age(obj.GetCreationTimestamp().Time),
		DotClass:    row.DotClass,
		Labels:      obj.GetLabels(),
		Annotations: obj.GetAnnotations(),
		YAML:        string(yamlBytes),
		YAMLURL:     fmt.Sprintf("/c/%s/%s/%s/%s/yaml", ctxName, nsOrDash(obj.GetNamespace()), rk.Name, obj.GetName()),
		YAMLDiffURL: fmt.Sprintf("/c/%s/%s/%s/%s/yaml/diff", ctxName, nsOrDash(obj.GetNamespace()), rk.Name, obj.GetName()),
		DeleteURL:   fmt.Sprintf("/c/%s/%s/%s/%s", ctxName, nsOrDash(obj.GetNamespace()), rk.Name, obj.GetName()),
	}

	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, item := range conditions {
		cm, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		t, _, _ := unstructured.NestedString(cm, "type")
		st, _, _ := unstructured.NestedString(cm, "status")
		reason, _, _ := unstructured.NestedString(cm, "reason")
		msg, _, _ := unstructured.NestedString(cm, "message")
		d.Conditions = append(d.Conditions, Condition{Type: t, Status: st, Reason: reason, Message: msg})
	}

	if rk.Name == "pods" {
		d.IsPod = true
		d.EvictURL = fmt.Sprintf("/c/%s/%s/%s/%s?evict=true", ctxName, nsOrDash(obj.GetNamespace()), rk.Name, obj.GetName())
		wsBase := fmt.Sprintf("/c/%s/%s/pods/%s", ctxName, obj.GetNamespace(), obj.GetName())
		d.LogsWSURLBase = wsBase + "/logs"
		d.ExecWSURLBase = wsBase + "/exec"
		d.PortForwardURL = wsBase + "/portforward"
		d.DefaultRemotePort = 80

		containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "containers")
		statuses, _, _ := unstructured.NestedSlice(obj.Object, "status", "containerStatuses")
		statusByName := map[string]map[string]interface{}{}
		for _, s := range statuses {
			sm, ok := s.(map[string]interface{})
			if !ok {
				continue
			}
			if n, _, _ := unstructured.NestedString(sm, "name"); n != "" {
				statusByName[n] = sm
			}
		}
		for _, item := range containers {
			cm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(cm, "name")
			image, _, _ := unstructured.NestedString(cm, "image")
			d.ContainerNames = append(d.ContainerNames, name)
			cont := Container{Name: name, Image: image}
			if sm, ok := statusByName[name]; ok {
				cont.Ready, _, _ = unstructured.NestedBool(sm, "ready")
				cont.Restarts, _, _ = unstructured.NestedInt64(sm, "restartCount")
				if _, ok, _ := unstructured.NestedMap(sm, "state", "running"); ok {
					cont.State = "Running"
				} else if w, ok, _ := unstructured.NestedMap(sm, "state", "waiting"); ok {
					cont.State, _, _ = unstructured.NestedString(w, "reason")
				} else if t, ok, _ := unstructured.NestedMap(sm, "state", "terminated"); ok {
					cont.State, _, _ = unstructured.NestedString(t, "reason")
				}
			}
			d.Containers = append(d.Containers, cont)
		}
	}

	if rk.Name == "services" {
		d.IsService = true
		ports, _, _ := unstructured.NestedSlice(obj.Object, "spec", "ports")
		if len(ports) > 0 {
			if pm, ok := ports[0].(map[string]interface{}); ok {
				if p, ok, _ := unstructured.NestedInt64(pm, "port"); ok {
					d.DefaultRemotePort = int32(p)
				}
			}
		}
	}

	if rk.Name == "deployments" {
		d.IsDeployment = true
		d.HistoryURL = fmt.Sprintf("/c/%s/%s/deployments/%s/history", ctxName, nsOrDash(obj.GetNamespace()), obj.GetName())
	}

	events, err := c.Clientset.CoreV1().Events(obj.GetNamespace()).Search(scheme.Scheme, obj)
	if err == nil {
		for _, ev := range events.Items {
			d.Events = append(d.Events, Event{Type: ev.Type, Reason: ev.Reason, Message: ev.Message, Age: age(ev.LastTimestamp.Time)})
		}
	}

	return d, nil
}

func (s *Server) handleYAMLApply(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource"), r.PathValue("name")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rawYAML := r.FormValue("yaml")

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	rk, ok := resolveKind(c, resource)
	if !ok {
		http.Error(w, "unknown resource: "+resource, http.StatusNotFound)
		return
	}

	obj := &unstructured.Unstructured{}
	jsonBytes, err := yaml.YAMLToJSON([]byte(rawYAML))
	if err != nil {
		triggerToast(w, "error", err.Error())
		template.HTMLEscape(w, []byte(err.Error()))
		return
	}
	if err := obj.UnmarshalJSON(jsonBytes); err != nil {
		triggerToast(w, "error", err.Error())
		template.HTMLEscape(w, []byte(err.Error()))
		return
	}

	ri := c.Dynamic.Resource(rk.GVR)
	var updateErr error
	if rk.Namespaced {
		_, updateErr = ri.Namespace(ns).Update(r.Context(), obj, metav1.UpdateOptions{})
	} else {
		_, updateErr = ri.Update(r.Context(), obj, metav1.UpdateOptions{})
	}
	if updateErr != nil {
		triggerToast(w, "error", updateErr.Error())
		template.HTMLEscape(w, []byte(updateErr.Error()))
		return
	}
	triggerToast(w, "success", fmt.Sprintf("%s %q applied", rk.Kind, name))
	fmt.Fprint(w, `<span class="dot dot-green"></span> applied`)
}

func (s *Server) handleResourceDelete(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource"), r.PathValue("name")

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	rk, ok := resolveKind(c, resource)
	if !ok {
		http.Error(w, "unknown resource: "+resource, http.StatusNotFound)
		return
	}

	verb := "deleted"
	if rk.Name == "pods" && r.URL.Query().Get("evict") == "true" {
		err = k8s.Evict(r.Context(), c, ns, name)
		verb = "evicted"
	} else {
		err = k8s.Delete(r.Context(), c, rk, ns, name)
	}
	if err != nil {
		triggerToast(w, "error", err.Error())
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	triggerToast(w, "success", fmt.Sprintf("%s %q %s", rk.Kind, name, verb))
}

func (s *Server) handleYAMLDiff(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource"), r.PathValue("name")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	rk, ok := resolveKind(c, resource)
	if !ok {
		http.Error(w, "unknown resource: "+resource, http.StatusNotFound)
		return
	}

	original := r.FormValue("original")
	edited := r.FormValue("yaml")
	lines := DiffLines(original, edited)
	changed := false
	for _, l := range lines {
		if l.Type != "eq" {
			changed = true
			break
		}
	}

	applyURL := fmt.Sprintf("/c/%s/%s/%s/%s/yaml", ctxName, nsOrDash(ns), rk.Name, name)
	s.render(w, "yamldiff", YAMLDiffData{Lines: lines, Changed: changed, ApplyURL: applyURL, YAML: edited})
}

func (s *Server) handleBulkDelete(w http.ResponseWriter, r *http.Request) {
	ctxName := r.PathValue("ctx")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	paths := r.Form["path"]
	deleted, failed := 0, 0
	for _, p := range paths {
		ns, resource, name, ok := parseResourcePath(ctxName, p)
		if !ok {
			failed++
			continue
		}
		rk, ok := resolveKind(c, resource)
		if !ok {
			failed++
			continue
		}
		if err := k8s.Delete(r.Context(), c, rk, ns, name); err != nil {
			failed++
			continue
		}
		deleted++
	}

	level := "success"
	if failed > 0 {
		level = "error"
	}
	msg := fmt.Sprintf("%d deleted", deleted)
	if failed > 0 {
		msg += fmt.Sprintf(", %d failed", failed)
	}
	triggerToast(w, level, msg)
}

// parseResourcePath splits a DetailURL of the form
// "/c/{ctx}/{ns}/{resource}/{name}" back into its parts, verifying it
// belongs to the given encoded context id.
func parseResourcePath(ctxID, path string) (ns, resource, name string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "c" || parts[1] != ctxID {
		return "", "", "", false
	}
	return parts[2], parts[3], parts[4], true
}
