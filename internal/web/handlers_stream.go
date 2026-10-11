package web

import (
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/rusik69/kui/internal/k8s"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true }, // local tool, single user
}

func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if resource == "all" || resource == "overview" {
		<-r.Context().Done()
		return
	}

	c, err := s.clients(ctxName)
	if err != nil {
		return
	}
	rk, ok := resolveKind(c, resource)
	if !ok {
		return
	}

	events, err := k8s.Watch(r.Context(), c, rk, ns)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}
	for range events {
		fmt.Fprint(w, "event: message\ndata: changed\n\n")
		flusher.Flush()
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, name := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("name")
	container := r.URL.Query().Get("container")

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

	tail := int64(500)
	stream, err := k8s.StreamLogs(r.Context(), c, ns, name, k8s.LogOptions{
		Container: container, Follow: true, TailLines: &tail,
	})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
		return
	}
	defer stream.Close()

	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if werr := conn.WriteMessage(websocket.TextMessage, buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
