package web

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rusik69/kui/internal/k8s"
)

const (
	maxExecMessageBytes = 1 << 20
	maxLogTailLines     = 5000
	wsWriteTimeout      = 10 * time.Second
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// ssePingInterval is how often the watch stream emits a keepalive comment
// while idle. Besides keeping the connection warm through proxies, the ping
// write is how the server learns the browser has gone away (see
// streamEvents): without it, a tab closed while a table was open would hold
// its API-server watch until the apiserver's own watch timeout.
const ssePingInterval = 15 * time.Second

func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	var events <-chan k8s.RowEvent
	if resource != "all" && resource != "overview" {
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
		events, err = k8s.Watch(r.Context(), c, rk, ns)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if events == nil {
		s.pingLoop(w, flusher, r.Context())
		return
	}
	s.streamEvents(w, flusher, r.Context(), events)
}

// pingLoop keeps the stream open with periodic comments until the client
// goes away.
func (s *Server) pingLoop(w http.ResponseWriter, flusher http.Flusher, ctx context.Context) {
	ticker := time.NewTicker(ssePingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.writeSSE(w, flusher, ": ping\n\n") {
				return
			}
		}
	}
}

// streamEvents forwards watch events to the client as SSE "message" events
// (the browser's EventSource then re-fetches the list; see app.js's
// syncWatchSource) while periodically pinging. It returns when:
//   - the request context is cancelled (browser closed/navigated away), or
//   - a write fails (client gone), or
//   - the upstream watch ends (the API server expires idle watches); the
//     closed stream makes the client reconnect, which starts a fresh watch.
func (s *Server) streamEvents(w http.ResponseWriter, flusher http.Flusher, ctx context.Context, events <-chan k8s.RowEvent) {
	if s.writeSSE(w, flusher, "event: message\ndata: changed\n\n") {
		return
	}
	ticker := time.NewTicker(ssePingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.writeSSE(w, flusher, ": ping\n\n") {
				return
			}
		case _, ok := <-events:
			if !ok {
				return
			}
			if s.writeSSE(w, flusher, "event: message\ndata: changed\n\n") {
				return
			}
		}
	}
}

// writeSSE writes one SSE payload and flushes. It reports whether the write
// failed (client gone), so callers can tear the stream down.
func (s *Server) writeSSE(w http.ResponseWriter, flusher http.Flusher, payload string) (failed bool) {
	if _, err := io.WriteString(w, payload); err != nil {
		return true
	}
	flusher.Flush()
	return false
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("name")
	container := r.URL.Query().Get("container")
	tail := int64(500)
	if rawTail := r.URL.Query().Get("tailLines"); rawTail != "" {
		value, err := strconv.ParseInt(rawTail, 10, 64)
		if err != nil || value < 1 || value > maxLogTailLines {
			http.Error(w, "tailLines must be between 1 and 5000", http.StatusBadRequest)
			return
		}
		tail = value
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
	writer := &wsWriter{conn: conn}

	stream, err := k8s.StreamLogs(r.Context(), c, ns, name, k8s.LogOptions{
		Container: container, Follow: true, TailLines: &tail,
	})
	if err != nil {
		_, _ = writer.Write([]byte("error: " + err.Error()))
		return
	}
	defer stream.Close()

	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if _, werr := writer.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
