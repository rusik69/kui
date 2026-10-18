package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"

	"github.com/rusik69/kui/internal/k8s"
)

const (
	maxYAMLFormBytes = 1 << 20
	maxDiffFormBytes = 256 << 10
	maxBulkFormBytes = 64 << 10
)

const defaultResourcePageSize int64 = 200
const maxResourcePageSize int64 = 1000

func parseFormLimit(w http.ResponseWriter, r *http.Request, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseForm(); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return false
	}
	return true
}
func nsOrDash(ns string) string {
	if ns == "" {
		return "_"
	}
	return ns
}

// resourcePath builds the base "/c/{ctx}/{ns}/{resource}/{name}" URL shared
// by DetailURL/YAMLDiffURL/DeleteURL/EvictURL/the YAML-diff apply URL —
// callers append their own suffix (e.g. "/yaml", "?evict=true").
func resourcePath(ctxName, namespace, resourceName, objName string) string {
	return fmt.Sprintf("/c/%s/%s/%s/%s", ctxName, nsOrDash(namespace), resourceName, objName)
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

	listOptions, err := parseResourceListOptions(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	table, err := s.buildTableData(r.Context(), c, rk, ctxName, ns, listOptions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if table.NextContinue != "" {
		table.NextPageURL = resourceListURL(r.URL.Path, listOptions.LabelSelector, listOptions.FieldSelector, listOptions.Limit, table.NextContinue)
	}

	switch {
	case r.URL.Query().Get("frame") == "table":
		s.render(w, "table", table)
	case r.Header.Get("HX-Request") == "true":
		data, err := s.buildPageData(r.Context(), ctxName, ns, resource)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		data.ListURL = resourceListURL(r.URL.Path, listOptions.LabelSelector, listOptions.FieldSelector, listOptions.Limit, listOptions.Continue)
		s.render(w, "content", data)
	default:
		data, err := s.buildPageData(r.Context(), ctxName, ns, resource)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		data.ListURL = resourceListURL(r.URL.Path, listOptions.LabelSelector, listOptions.FieldSelector, listOptions.Limit, listOptions.Continue)
		s.render(w, "page", data)
	}
}

func parseResourceListOptions(query url.Values) (metav1.ListOptions, error) {
	labelSelector, fieldSelector := query.Get("labelSelector"), query.Get("fieldSelector")
	if _, err := labels.Parse(labelSelector); err != nil {
		return metav1.ListOptions{}, fmt.Errorf("invalid label selector: %w", err)
	}
	if _, err := fields.ParseSelector(fieldSelector); err != nil {
		return metav1.ListOptions{}, fmt.Errorf("invalid field selector: %w", err)
	}
	limit := defaultResourcePageSize
	if rawLimit := query.Get("limit"); rawLimit != "" {
		parsed, err := strconv.ParseInt(rawLimit, 10, 64)
		if err != nil || parsed < 1 || parsed > maxResourcePageSize {
			return metav1.ListOptions{}, fmt.Errorf("limit must be between 1 and %d", maxResourcePageSize)
		}
		limit = parsed
	}
	return metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: fieldSelector,
		Limit:         limit,
		Continue:      query.Get("continue"),
	}, nil
}

func resourceListURL(path, labelSelector, fieldSelector string, limit int64, continueToken string) string {
	query := make(url.Values)
	query.Set("frame", "table")
	query.Set("limit", strconv.FormatInt(limit, 10))
	if labelSelector != "" {
		query.Set("labelSelector", labelSelector)
	}
	if fieldSelector != "" {
		query.Set("fieldSelector", fieldSelector)
	}
	if continueToken != "" {
		query.Set("continue", continueToken)
	}
	return path + "?" + query.Encode()
}

func tableHeaderColumnCount(columns []string, showNamespace, selectable bool) int {
	count := len(columns)
	if showNamespace {
		count++
	}
	if selectable {
		count++
	}
	return count
}

func (s *Server) buildTableData(ctx context.Context, c *k8s.Clients, rk k8s.ResourceKind, ctxName, ns string, options metav1.ListOptions) (TableData, error) {
	list, err := k8s.ListWithOptions(ctx, c, rk, ns, options)
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
		row.DetailURL = resourcePath(ctxName, obj.GetNamespace(), rk.Name, obj.GetName())
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
		Kind:              rk.Kind,
		Columns:           columns,
		Rows:              rows,
		ShowNamespace:     showNS,
		HeaderColumnCount: tableHeaderColumnCount(columns, showNS, true),
		MetricsURL:        metricsURL,
		Selectable:        true,
		HasMetrics:        hasMetrics,
		LabelSelector:     options.LabelSelector,
		FieldSelector:     options.FieldSelector,
		PageSize:          options.Limit,
		NextContinue:      list.GetContinue(),
	}, nil
}

func (s *Server) renderAllResourcesDirectory(w http.ResponseWriter, r *http.Request, ctxName, ns string, c *k8s.Clients) {
	kinds, err := k8s.AllAPIResources(c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	table := TableData{Kind: "All resources", Columns: []string{"Kind", "Namespaced"}, HeaderColumnCount: 2, RowsAreKinds: true}
	for _, rk := range kinds {
		table.Rows = append(table.Rows, Row{
			Name:      rk.Kind,
			Cells:     []string{rk.Kind, fmt.Sprintf("%v", rk.Namespaced)},
			DetailURL: fmt.Sprintf("/c/%s/%s/%s", ctxName, ns, rk.Name),
		})
	}

	if r.Header.Get("HX-Request") == "true" && r.URL.Query().Get("frame") != "table" {
		data, err := s.buildPageData(r.Context(), ctxName, ns, "all")
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
	data, err := s.buildPageData(r.Context(), ctxName, ns, "all")
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
		YAMLDiffURL: resourcePath(ctxName, obj.GetNamespace(), rk.Name, obj.GetName()) + "/yaml/diff",
		DeleteURL:   resourcePath(ctxName, obj.GetNamespace(), rk.Name, obj.GetName()),
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
		d.EvictURL = resourcePath(ctxName, obj.GetNamespace(), rk.Name, obj.GetName()) + "?evict=true"
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

	if rk.Name == "nodes" {
		d.IsNode = true

		addrs, _, _ := unstructured.NestedSlice(obj.Object, "status", "addresses")
		for _, item := range addrs {
			am, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			t, _, _ := unstructured.NestedString(am, "type")
			a, _, _ := unstructured.NestedString(am, "address")
			d.Addresses = append(d.Addresses, NodeAddress{Type: t, Address: a})
		}

		d.NodeInfo.OS, _, _ = unstructured.NestedString(obj.Object, "status", "nodeInfo", "operatingSystem")
		d.NodeInfo.Arch, _, _ = unstructured.NestedString(obj.Object, "status", "nodeInfo", "architecture")
		d.NodeInfo.Kernel, _, _ = unstructured.NestedString(obj.Object, "status", "nodeInfo", "kernelVersion")
		d.NodeInfo.Runtime, _, _ = unstructured.NestedString(obj.Object, "status", "nodeInfo", "containerRuntimeVersion")

		taints, _, _ := unstructured.NestedSlice(obj.Object, "spec", "taints")
		for _, item := range taints {
			tm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			key, _, _ := unstructured.NestedString(tm, "key")
			value, _, _ := unstructured.NestedString(tm, "value")
			effect, _, _ := unstructured.NestedString(tm, "effect")
			d.Taints = append(d.Taints, NodeTaint{Key: key, Value: value, Effect: effect})
		}

		d.CPUAllocatable, _, _ = unstructured.NestedString(obj.Object, "status", "allocatable", "cpu")
		d.MemAllocatable, _, _ = unstructured.NestedString(obj.Object, "status", "allocatable", "memory")
		d.PodsAllocatable, _, _ = unstructured.NestedString(obj.Object, "status", "allocatable", "pods")

		pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			FieldSelector: "spec.nodeName=" + obj.GetName(),
		})
		if err == nil {
			for _, p := range pods.Items {
				d.PodsOnNode = append(d.PodsOnNode, PodOnNode{
					Name:      p.Name,
					Namespace: p.Namespace,
					Status:    podStateTyped(p),
					DetailURL: fmt.Sprintf("/c/%s/%s/pods/%s", ctxName, p.Namespace, p.Name),
				})
			}
		}
	}

	if rk.Name == "secrets" {
		d.IsSecret = true
		data, _, _ := unstructured.NestedMap(obj.Object, "data")
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v, _ := data[k].(string)
			d.SecretData = append(d.SecretData, SecretEntry{Key: k, Value: v})
		}
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
	if !parseFormLimit(w, r, maxYAMLFormBytes) {
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

	if err := validateYAMLIdentity(obj, rk, ns, name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

func validateYAMLIdentity(obj *unstructured.Unstructured, rk k8s.ResourceKind, namespace, name string) error {
	if obj.GetKind() != rk.Kind {
		return fmt.Errorf("manifest kind %q does not match resource kind %q", obj.GetKind(), rk.Kind)
	}
	if obj.GetName() != name {
		return fmt.Errorf("manifest name %q does not match resource name %q", obj.GetName(), name)
	}
	if rk.Namespaced && obj.GetNamespace() != namespace {
		return fmt.Errorf("manifest namespace %q does not match resource namespace %q", obj.GetNamespace(), namespace)
	}
	if !rk.Namespaced && obj.GetNamespace() != "" {
		return fmt.Errorf("cluster-scoped resource manifest must not set a namespace")
	}
	return nil
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

	action, verb := "Delete", "deleted"
	if rk.Name == "pods" && r.URL.Query().Get("evict") == "true" {
		err = k8s.Evict(r.Context(), c, ns, name)
		action, verb = "Evict", "evicted"
	} else {
		err = k8s.Delete(r.Context(), c, rk, ns, name)
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("%s %s %q failed: %v", action, rk.Kind, name, err), http.StatusBadGateway)
		return
	}
	triggerToast(w, "success", fmt.Sprintf("%s %q %s", rk.Kind, name, verb))
}

func (s *Server) handleYAMLDiff(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource"), r.PathValue("name")
	if !parseFormLimit(w, r, maxDiffFormBytes) {
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
	if changed {
		obj := &unstructured.Unstructured{}
		jsonBytes, err := yaml.YAMLToJSON([]byte(edited))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := obj.UnmarshalJSON(jsonBytes); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := validateYAMLIdentity(obj, rk, ns, name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		opts := metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}}
		ri := c.Dynamic.Resource(rk.GVR)
		if rk.Namespaced {
			_, err = ri.Namespace(ns).Update(r.Context(), obj, opts)
		} else {
			_, err = ri.Update(r.Context(), obj, opts)
		}
		if err != nil {
			status := http.StatusBadGateway
			if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
				status = http.StatusUnprocessableEntity
			}
			http.Error(w, "server-side dry-run failed: "+err.Error(), status)
			return
		}
	}
	applyURL := resourcePath(ctxName, ns, rk.Name, name) + "/yaml"
	s.render(w, "yamldiff", YAMLDiffData{Lines: lines, Changed: changed, ApplyURL: applyURL, YAML: edited})
}

func (s *Server) handleBulkDelete(w http.ResponseWriter, r *http.Request) {
	ctxName := r.PathValue("ctx")
	if !parseFormLimit(w, r, maxBulkFormBytes) {
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
	if path == "" || strings.ContainsAny(path, "?#") {
		return "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "c" || parts[1] != ctxID {
		return "", "", "", false
	}
	for _, part := range parts[2:] {
		if part == "" || part == "." || part == ".." {
			return "", "", "", false
		}
	}
	return parts[2], parts[3], parts[4], true
}
