package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rusik69/kui/internal/k8s"
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
		Pod: podName, Namespace: ns,
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
