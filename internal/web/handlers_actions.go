package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rusik69/kui/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
)

// wsReader/wsWriter adapt a gorilla websocket connection to io.Reader/io.Writer
// so it can be plugged directly into remotecommand's stdio pipes.
type wsReader struct {
	conn *websocket.Conn
	buf  []byte
}

func newWSReader(conn *websocket.Conn) *wsReader {
	conn.SetReadLimit(maxExecMessageBytes)
	return &wsReader{conn: conn}
}

func (r *wsReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		_, data, err := r.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		r.buf = data
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

type wsWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *wsWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)); err != nil {
		return 0, err
	}
	if err := w.conn.WriteMessage(websocket.TextMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("name")
	container := r.URL.Query().Get("container")
	command := r.URL.Query().Get("command")
	if command == "" {
		command = "/bin/sh"
	}

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	rw := &wsWriter{conn: conn}
	rr := newWSReader(conn)
	err = k8s.Exec(r.Context(), c, ns, name, k8s.ExecOptions{
		Container: container,
		Command:   []string{command},
		Stdin:     rr,
		Stdout:    rw,
		Stderr:    rw,
		TTY:       true,
	})
	if err != nil {
		_, _ = rw.Write([]byte("\r\nexec error: " + err.Error() + "\r\n"))
	}
}

func (s *Server) handleDebugShell(w http.ResponseWriter, r *http.Request) {
	ctxName, ns := r.PathValue("ctx"), r.PathValue("ns")
	if ns == "" || ns == "_all" {
		http.Error(w, "select a namespace before starting a debug shell", http.StatusBadRequest)
		return
	}

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Retire finished debug pods without disrupting sessions in other tabs.
	if err := k8s.CleanupEphemeralDebugPods(r.Context(), c, ns); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	pod := k8s.DebugPodShellSpec("", ns, s.debugImage)
	pod.GenerateName = "kui-debug-"
	s.startDebugShell(w, r, c, ctxName, ns, pod, "")
}

func (s *Server) handleNodeDebugShell(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, nodeName := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("node")
	if ns == "" || ns == "_" || ns == "_all" {
		http.Error(w, "select a namespace before starting a node shell", http.StatusBadRequest)
		return
	}
	if nodeName == "" {
		http.Error(w, "node name is required", http.StatusBadRequest)
		return
	}

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := k8s.CleanupEphemeralDebugPods(r.Context(), c, ns); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	pod := k8s.NodeDebugShellSpec(ns, nodeName, s.debugImage)
	s.startDebugShell(w, r, c, ctxName, ns, pod, nodeName)
}

func (s *Server) startDebugShell(w http.ResponseWriter, r *http.Request, c *k8s.Clients, ctxName, ns string, pod *corev1.Pod, nodeName string) {
	created, err := c.Clientset.CoreV1().Pods(ns).Create(r.Context(), pod, metav1.CreateOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	podName := created.Name

	if err := waitForDebugPod(r.Context(), func(ctx context.Context) (*corev1.Pod, error) {
		return c.Clientset.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
	}); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.Clientset.CoreV1().Pods(ns).Delete(cleanupCtx, podName, metav1.DeleteOptions{})
		if errors.Is(err, context.Canceled) {
			return
		}
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		http.Error(w, "debug pod did not become ready: "+err.Error(), status)
		return
	}

	s.render(w, "debugshell", DebugShellData{
		Pod: podName, Namespace: ns, NodeName: nodeName,
		ExecWSURLBase: fmt.Sprintf("/c/%s/%s/pods/%s/exec", ctxName, ns, podName),
		KillURL:       fmt.Sprintf("/c/%s/%s/debug-shell/%s/kill", ctxName, ns, podName),
	})
}
func waitForDebugPod(ctx context.Context, get func(context.Context) (*corev1.Pod, error)) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		pod, err := get(ctx)
		if err == nil && pod.Status.Phase == corev1.PodRunning {
			for _, status := range pod.Status.ContainerStatuses {
				if status.Name == "shell" && status.Ready {
					return nil
				}
			}
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// handleDebugShellKill terminates a kui ephemeral debug pod. The shell
// container is a plain sleep, so a hard delete works; the label check in
// KillDebugPod keeps this from ever touching a non-kui pod.
func (s *Server) handleDebugShellKill(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, pod := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("pod")

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := k8s.KillDebugPod(r.Context(), c, ns, pod); err != nil {
		triggerToast(w, "error", err.Error())
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	triggerToast(w, "success", fmt.Sprintf("debug pod %q killed", pod))
	// Intentionally empty body: swapping it into #detail-pane leaves an empty
	// pane, and app.js's afterSwap handler closes an emptied detail pane.
}

func (s *Server) handlePortForwardStart(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	localPort, err1 := strconv.Atoi(r.FormValue("localPort"))
	remotePort, err2 := strconv.Atoi(r.FormValue("remotePort"))
	if err1 != nil || err2 != nil || localPort < 1 || localPort > 65535 || remotePort < 1 || remotePort > 65535 {
		http.Error(w, "ports must be between 1 and 65535", http.StatusBadRequest)
		return
	}
	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	handle, err := s.forwards.Start(c, ns, name, localPort, remotePort, discardWriter{}, discardWriter{})
	if err != nil {
		triggerToast(w, "error", err.Error())
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `<span class="error-state">%s</span>`, template.HTMLEscapeString(err.Error()))
		return
	}
	triggerToast(w, "success", fmt.Sprintf("forwarding localhost:%d to %s:%d", handle.LocalPort, name, handle.RemotePort))
	fmt.Fprintf(w, `localhost:%d &rarr; %s:%d <button class="btn btn-danger" hx-delete="/portforward/%s" hx-swap="outerHTML" hx-target="closest div">Stop</button>`,
		handle.LocalPort, name, handle.RemotePort, handle.ID)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func (s *Server) handlePortForwardStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Stop reports false when the tunnel is already gone (it died on its own
	// and was reaped, or was stopped before): say so instead of claiming a
	// forward was stopped that no longer exists.
	if s.forwards.Stop(id) {
		triggerToast(w, "success", "port-forward stopped")
		_, _ = w.Write([]byte("stopped"))
		return
	}
	triggerToast(w, "success", "port-forward no longer running")
	_, _ = w.Write([]byte("stopped"))
}

type rowUsage struct {
	CPU string `json:"cpu"`
	Mem string `json:"mem"`
}

// writeMetricsScript renders the aggregate strip plus a per-row patch script
// consumed by app.js's kui.applyMetrics. encoding/json escapes HTML-sensitive
// characters in the JSON payload so names cannot terminate the script element.
func writeMetricsScript(w http.ResponseWriter, totalCPU, totalMem int64, byKey map[string]rowUsage) {
	fmt.Fprintf(w, `<div class="toolbar" style="border:none"><span>&Sigma; CPU: %dm</span><span>&Sigma; Mem: %dMi</span></div>`, totalCPU, totalMem)
	payload, err := json.Marshal(byKey)
	if err != nil {
		return
	}
	fmt.Fprintf(w, `<script>kui.applyMetrics(%s)</script>`, payload)
}

func (s *Server) handleMetricsPods(w http.ResponseWriter, r *http.Request) {
	ctxName := r.PathValue("ctx")
	ns := r.URL.Query().Get("ns")
	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	list, ok, err := k8s.PodMetrics(r.Context(), c, ns)
	if err != nil {
		http.Error(w, "pod metrics request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !ok {
		http.Error(w, "pod metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	if list == nil || len(list.Items) == 0 {
		return
	}
	totalCPU, totalMem := int64(0), int64(0)
	byKey := make(map[string]rowUsage, len(list.Items))
	for _, pm := range list.Items {
		podCPU, podMem := int64(0), int64(0)
		for _, cm := range pm.Containers {
			podCPU += cm.Usage.Cpu().MilliValue()
			podMem += cm.Usage.Memory().Value() / (1024 * 1024)
		}
		totalCPU += podCPU
		totalMem += podMem
		key := pm.Namespace + "/" + pm.Name
		byKey[key] = rowUsage{CPU: fmt.Sprintf("%dm", podCPU), Mem: fmt.Sprintf("%dMi", podMem)}
	}
	writeMetricsScript(w, totalCPU, totalMem, byKey)
}

func (s *Server) handleMetricsNodes(w http.ResponseWriter, r *http.Request) {
	ctxName := r.PathValue("ctx")
	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	list, ok, err := k8s.NodeMetrics(r.Context(), c)
	if err != nil {
		http.Error(w, "node metrics request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !ok {
		http.Error(w, "node metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	if list == nil || len(list.Items) == 0 {
		return
	}
	totalCPU, totalMem := int64(0), int64(0)
	byKey := make(map[string]rowUsage, len(list.Items))
	for _, nm := range list.Items {
		cpu := nm.Usage.Cpu().MilliValue()
		mem := nm.Usage.Memory().Value() / (1024 * 1024)
		totalCPU += cpu
		totalMem += mem
		byKey[nm.Name] = rowUsage{CPU: fmt.Sprintf("%dm", cpu), Mem: fmt.Sprintf("%dMi", mem)}
	}
	writeMetricsScript(w, totalCPU, totalMem, byKey)
}

type revisionView struct {
	Revision  int
	Name      string
	Image     string
	Replicas  int64
	Created   string
	IsCurrent bool
}

func (s *Server) handleDeploymentHistory(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("name")
	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	revisions, err := k8s.DeploymentHistory(r.Context(), c, ns, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	views := make([]revisionView, 0, len(revisions))
	for _, rv := range revisions {
		views = append(views, revisionView{
			Revision: rv.Revision, Name: rv.Name, Image: rv.Image,
			Replicas: rv.Replicas, Created: age(rv.Created.Time), IsCurrent: rv.IsCurrent,
		})
	}
	s.render(w, "history", views)
}

const maxWorkloadBody = 16 << 10
const maxMetadataBatch = 100

type deploymentRequest struct {
	Replicas *int32 `json:"replicas"`
	Action   string `json:"action"`
	Revision int64  `json:"revision"`
}

func (s *Server) handleDeploymentScale(w http.ResponseWriter, r *http.Request) {
	var req deploymentRequest
	if !decodeJSONBody(w, r, maxWorkloadBody, &req) || req.Replicas == nil || *req.Replicas < 0 || *req.Replicas > 100000 {
		http.Error(w, "replicas must be an integer from 0 to 100000", http.StatusBadRequest)
		return
	}
	c, err := s.clients(r.PathValue("ctx"))
	if err == nil {
		err = k8s.ScaleDeployment(r.Context(), c, r.PathValue("ns"), r.PathValue("name"), *req.Replicas)
	}
	if err != nil {
		writeActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "replicas": *req.Replicas})
}

func (s *Server) handleDeploymentAction(w http.ResponseWriter, r *http.Request) {
	var req deploymentRequest
	if !decodeJSONBody(w, r, maxWorkloadBody, &req) || (req.Action != "restart" && req.Action != "pause" && req.Action != "resume" && req.Action != "rollback") {
		http.Error(w, "invalid Deployment action", http.StatusBadRequest)
		return
	}
	if req.Action == "rollback" && req.Revision < 1 {
		http.Error(w, "a positive revision is required", http.StatusBadRequest)
		return
	}
	c, err := s.clients(r.PathValue("ctx"))
	if err == nil {
		err = k8s.DeploymentAction(r.Context(), c, r.PathValue("ns"), r.PathValue("name"), req.Action, req.Revision)
	}
	if err != nil {
		writeActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": req.Action})
}

type metadataRequest struct {
	Paths   []string `json:"paths"`
	Kind    string   `json:"kind"`
	Action  string   `json:"action"`
	Key     string   `json:"key"`
	Value   string   `json:"value"`
	Preview bool     `json:"preview"`
}

type metadataOutcome struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Patch   any    `json:"patch,omitempty"`
	Error   string `json:"error,omitempty"`
	Success bool   `json:"success"`
}
type metadataPath struct{ path, ns, resource, name string }

func (s *Server) handleBulkMetadata(w http.ResponseWriter, r *http.Request) {
	var req metadataRequest
	if !decodeJSONBody(w, r, 64<<10, &req) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Paths) == 0 || len(req.Paths) > maxMetadataBatch || (req.Kind != "labels" && req.Kind != "annotations") ||
		(req.Action != "add" && req.Action != "update" && req.Action != "remove") {
		http.Error(w, "invalid metadata batch", http.StatusBadRequest)
		return
	}
	if errs := validation.IsQualifiedName(req.Key); len(errs) > 0 || req.Key == "" {
		http.Error(w, "invalid metadata key", http.StatusBadRequest)
		return
	}
	if req.Action != "remove" {
		if req.Kind == "labels" && len(validation.IsValidLabelValue(req.Value)) > 0 {
			http.Error(w, "invalid label value", http.StatusBadRequest)
			return
		}
		if req.Kind == "annotations" && len(req.Value) > 256*1024 {
			http.Error(w, "annotation value is too large", http.StatusBadRequest)
			return
		}
	}
	ctxName := r.PathValue("ctx")
	parsed := make([]metadataPath, 0, len(req.Paths))
	seen := map[string]bool{}
	for _, p := range req.Paths {
		ns, resource, name, ok := parseResourcePath(ctxName, p)
		if !ok || seen[p] {
			http.Error(w, "invalid or duplicate resource path", http.StatusBadRequest)
			return
		}
		seen[p] = true
		parsed = append(parsed, metadataPath{p, ns, resource, name})
	}
	c, err := s.clients(ctxName)
	if err != nil {
		writeActionError(w, err)
		return
	}
	outcomes := performMetadataBatch(r.Context(), c, req, parsed)
	writeJSON(w, http.StatusOK, map[string]any{"outcomes": outcomes, "preview": req.Preview})
}

func performMetadataBatch(ctx context.Context, c *k8s.Clients, req metadataRequest, parsed []metadataPath) []metadataOutcome {
	outcomes := make([]metadataOutcome, 0, len(parsed))
	for _, item := range parsed {
		rk, ok := k8s.ByName(item.resource)
		if !ok {
			outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Error: "unsupported resource kind"})
			continue
		}
		resource := c.Dynamic.Resource(rk.GVR)
		var client dynamic.ResourceInterface = resource
		if rk.Namespaced {
			if item.ns == "_" {
				outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Error: "namespace required"})
				continue
			}
			client = resource.Namespace(item.ns)
		} else if item.ns != "_" {
			outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Error: "cluster resource path has namespace"})
			continue
		}
		obj, getErr := client.Get(ctx, item.name, metav1.GetOptions{})
		if getErr != nil {
			outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Error: getErr.Error()})
			continue
		}
		metadata := map[string]any{"resourceVersion": obj.GetResourceVersion()}
		patchValues := map[string]any{}
		current := obj.GetLabels()
		if req.Kind == "annotations" {
			current = obj.GetAnnotations()
		}
		_, exists := current[req.Key]
		switch req.Action {
		case "add":
			if exists {
				outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Error: "key already exists"})
				continue
			}
			patchValues[req.Key] = req.Value
		case "update":
			if !exists {
				outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Error: "key does not exist"})
				continue
			}
			patchValues[req.Key] = req.Value
		case "remove":
			if !exists {
				outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Error: "key does not exist"})
				continue
			}
			patchValues[req.Key] = nil
		default:
			outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Error: "invalid action"})
			continue
		}
		metadata[req.Kind] = patchValues
		patch := map[string]any{"metadata": metadata}
		if req.Preview {
			outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Patch: patch, Success: true})
			continue
		}
		data, _ := json.Marshal(patch)
		_, patchErr := client.Patch(ctx, item.name, types.MergePatchType, data, metav1.PatchOptions{})
		if patchErr != nil {
			outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Patch: patch, Error: patchErr.Error()})
		} else {
			outcomes = append(outcomes, metadataOutcome{Path: item.path, Name: item.name, Patch: patch, Success: true})
		}
	}
	return outcomes
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, limit int64, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(target) != nil {
		return false
	}
	var extra any
	return dec.Decode(&extra) == io.EOF
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeActionError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if apierrors.IsConflict(err) {
		status = http.StatusConflict
	} else if apierrors.IsNotFound(err) {
		status = http.StatusNotFound
	} else if apierrors.IsInvalid(err) {
		status = http.StatusUnprocessableEntity
	}
	http.Error(w, err.Error(), status)
}

func (s *Server) handleDeploymentStatus(w http.ResponseWriter, r *http.Request) {
	c, err := s.clients(r.PathValue("ctx"))
	if err != nil {
		writeActionError(w, err)
		return
	}
	deployment, err := c.Clientset.AppsV1().Deployments(r.PathValue("ns")).Get(r.Context(), r.PathValue("name"), metav1.GetOptions{})
	if err != nil {
		writeActionError(w, err)
		return
	}
	conditions := make([]map[string]string, 0, len(deployment.Status.Conditions))
	for _, condition := range deployment.Status.Conditions {
		conditions = append(conditions, map[string]string{"type": string(condition.Type), "status": string(condition.Status), "reason": condition.Reason, "message": condition.Message})
	}
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	revisions, historyErr := k8s.DeploymentHistory(r.Context(), c, r.PathValue("ns"), r.PathValue("name"))
	historyMessage := ""
	if historyErr != nil {
		historyMessage = historyErr.Error()
	}
	availableRevisions := make([]int, 0, len(revisions))
	for _, revision := range revisions {
		availableRevisions = append(availableRevisions, revision.Revision)
	}
	writeJSON(w, http.StatusOK, map[string]any{"desired": desired, "current": deployment.Status.Replicas, "updated": deployment.Status.UpdatedReplicas, "available": deployment.Status.AvailableReplicas, "paused": deployment.Spec.Paused, "conditions": conditions, "revisions": availableRevisions, "historyError": historyMessage})
}
