package web

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rusik69/kui/internal/k8s"
)

// terminalManager tracks live local kubectl terminals so closing the browser
// tab (WebSocket gone) or shutting kui down reaps the shells and their
// kubeconfig snapshots instead of leaking processes.
type terminalManager struct {
	mu        sync.Mutex
	terminals map[*k8s.LocalTerminal]struct{}
}

func newTerminalManager() *terminalManager {
	return &terminalManager{terminals: map[*k8s.LocalTerminal]struct{}{}}
}

func (tm *terminalManager) add(t *k8s.LocalTerminal) {
	tm.mu.Lock()
	tm.terminals[t] = struct{}{}
	tm.mu.Unlock()
	go func() {
		<-t.Done()
		tm.mu.Lock()
		delete(tm.terminals, t)
		tm.mu.Unlock()
	}()
}

func (tm *terminalManager) remove(t *k8s.LocalTerminal) {
	tm.mu.Lock()
	delete(tm.terminals, t)
	tm.mu.Unlock()
	_ = t.Close()
}

func (tm *terminalManager) closeAll() {
	tm.mu.Lock()
	live := make([]*k8s.LocalTerminal, 0, len(tm.terminals))
	for t := range tm.terminals {
		live = append(live, t)
	}
	tm.terminals = map[*k8s.LocalTerminal]struct{}{}
	tm.mu.Unlock()
	for _, t := range live {
		_ = t.Close()
	}
}

// maxTerminalMessageBytes bounds one WebSocket frame from the browser
// (keystrokes are tiny; anything bigger is abuse or a bug).
const maxTerminalMessageBytes = 4 << 10

// terminalWriteDeadline bounds each PTY write.
const terminalWriteDeadline = 10 * time.Second

// handleTerminalPage renders the kubectl terminal panel for the current
// context. It is a content fragment like the resource list — sidebar URL,
// same shell.
func (s *Server) handleTerminalPage(w http.ResponseWriter, r *http.Request) {
	ctxName, ns := r.PathValue("ctx"), r.PathValue("ns")
	data, err := s.buildPageData(r.Context(), ctxName, ns, "terminal")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.render(w, "content", data)
}

// handleTerminal upgrades to a WebSocket and bridges it to a local shell
// whose KUBECONFIG points at a snapshot for the requested context. The
// browser sends keystrokes; the PTY streams output back. Either side closing
// tears the session down.
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	encCtx := r.PathValue("ctx")
	ctxName, err := decodeCtx(encCtx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// clients() validates the context resolves (and builds its client bundle)
	// before we spawn anything.
	if _, err := s.clients(encCtx); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	term, err := s.mgr.NewLocalTerminal(ctxName, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.terminals.add(term)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.terminals.remove(term)
		return
	}
	conn.SetReadLimit(maxTerminalMessageBytes)

	// Output pump: PTY → WebSocket. Ends when the shell exits or the PTY
	// closes (either side disconnecting).
	go func() {
		defer func() {
			s.terminals.remove(term)
			_ = conn.Close()
		}()
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				if werr := conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)); werr != nil {
					return
				}
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Input loop: WebSocket → PTY. A browser close or any read error ends
	// the session.
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if werr := conn.SetWriteDeadline(time.Now().Add(terminalWriteDeadline)); werr != nil {
			return
		}
		if _, err := term.Write(message); err != nil {
			return
		}
	}
}
