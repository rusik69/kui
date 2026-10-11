package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rusik69/kui/internal/k8s"
)

// wsReader/wsWriter adapt a gorilla websocket connection to io.Reader/io.Writer
// so it can be plugged directly into remotecommand's stdio pipes.
type wsReader struct {
	conn *websocket.Conn
	buf  []byte
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

type wsWriter struct{ conn *websocket.Conn }

func (w *wsWriter) Write(p []byte) (int, error) {
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
	rr := &wsReader{conn: conn}
	err = k8s.Exec(r.Context(), c, ns, name, k8s.ExecOptions{
		Container: container,
		Command:   []string{command},
		Stdin:     rr,
		Stdout:    rw,
		Stderr:    rw,
		TTY:       true,
	})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\nexec error: "+err.Error()+"\r\n"))
	}
}

func (s *Server) handleDebugShell(w http.ResponseWriter, r *http.Request) {
	ctxName, ns := r.PathValue("ctx"), r.PathValue("ns")

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	podName := fmt.Sprintf("kui-debug-%d", rand.Intn(1_000_000))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			Labels:    map[string]string{"kui.dev/ephemeral": "true"},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:    "shell",
				Image:   "busybox:1.36",
				Command: []string{"sleep", "86400"},
			}},
		},
	}
	if _, err := c.Clientset.CoreV1().Pods(ns).Create(r.Context(), pod, metav1.CreateOptions{}); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		p, err := c.Clientset.CoreV1().Pods(ns).Get(r.Context(), podName, metav1.GetOptions{})
		if err == nil && p.Status.Phase == corev1.PodRunning {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	s.render(w, "debugshell", DebugShellData{
		Pod: podName, Namespace: ns,
		ExecWSURLBase: fmt.Sprintf("/c/%s/%s/pods/%s/exec", ctxName, ns, podName),
	})
}

func (s *Server) handlePortForwardStart(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("name")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	localPort, err1 := strconv.Atoi(r.FormValue("localPort"))
	remotePort, err2 := strconv.Atoi(r.FormValue("remotePort"))
	if err1 != nil || err2 != nil {
		http.Error(w, "invalid port", http.StatusBadRequest)
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
	s.forwards.Stop(id)
	_, _ = w.Write([]byte("stopped"))
}

type rowUsage struct {
	CPU string `json:"cpu"`
	Mem string `json:"mem"`
}

// writeMetricsScript renders the aggregate strip plus a per-row patch script
// consumed by app.js's kui.applyMetrics. Per-row data is only ever injected
// as machine-generated k8s resource names/numbers (DNS-1123 constrained, so
// it can't contain "<"/">"/"/" to break out of the <script> tag) — safe to
// embed directly without html/template's JS-context escaping machinery.
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
		return
	}
	list, ok := k8s.PodMetrics(r.Context(), c, ns)
	if !ok || len(list.Items) == 0 {
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
		return
	}
	list, ok := k8s.NodeMetrics(r.Context(), c)
	if !ok || len(list.Items) == 0 {
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
