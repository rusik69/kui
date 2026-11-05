package k8s

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

// LocalTerminal is one interactive shell whose environment points kubectl at
// a snapshot kubeconfig for the selected context. It runs kui's own user's
// shell as a child of the kui process: kui is a local, single-user tool and
// already holds every credential the shell could reach, so the terminal adds
// no new privilege — it only scopes KUBECONFIG to the selected context.
type LocalTerminal struct {
	cmd     *exec.Cmd
	ptmx    *os.File
	kcfg    string // snapshot kubeconfig path + its temp dir
	kcfgDir string

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

// NewLocalTerminal starts an interactive shell with KUBECONFIG pointing at a
// snapshot kubeconfig for ctxName. shell overrides the user's shell when
// non-empty (tests); empty means $SHELL, then /bin/sh, then /bin/bash.
// The returned terminal streams through its PTY file; Wait reaps the child
// after the PTY is closed or the shell exits.
func (m *Manager) NewLocalTerminal(ctxName, shell string) (*LocalTerminal, error) {
	snapshot, err := m.SnapshotKubeconfig(ctxName)
	if err != nil {
		return nil, err
	}
	t := &LocalTerminal{
		kcfg:    snapshot,
		kcfgDir: filepath.Dir(snapshot),
		done:    make(chan struct{}),
	}
	shellName := shell
	if shellName == "" {
		shellName = strings.TrimSpace(os.Getenv("SHELL"))
	}
	if shellName == "" {
		if runtime.GOOS == "windows" {
			shellName = "powershell"
		} else {
			if _, err := exec.LookPath("/bin/bash"); err == nil {
				shellName = "/bin/bash"
			} else {
				shellName = "/bin/sh"
			}
		}
	}
	cmd := exec.Command(shellName, "-i")
	cmd.Env = append(os.Environ(),
		"KUBECONFIG="+snapshot,
		// A stable, identifiable prompt prefix: users see which context the
		// shell talks to even with several kui windows open.
		"KUI_CONTEXT="+ctxName,
		"TERM=xterm-256color",
	)
	// Input echo: PTY handles it; make sure the shell is interactive-ish.
	cmd.Dir = os.Getenv("HOME")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		_ = os.RemoveAll(t.kcfgDir)
		return nil, fmt.Errorf("start shell %q: %w", shellName, err)
	}
	t.cmd, t.ptmx = cmd, ptmx
	go func() {
		_ = cmd.Wait()
		close(t.done)
	}()
	return t, nil
}

// SnapshotPath returns the snapshot kubeconfig file path backing this
// terminal's KUBECONFIG.
func (t *LocalTerminal) SnapshotPath() string {
	return t.kcfg
}

// Read reads from the PTY (shell output). Called from the WebSocket pump.
func (t *LocalTerminal) Read(p []byte) (int, error) {
	return t.ptmx.Read(p)
}

// Write sends user keystrokes to the shell.
func (t *LocalTerminal) Write(p []byte) (int, error) {
	return t.ptmx.Write(p)
}

// Done reports the shell's exit.
func (t *LocalTerminal) Done() <-chan struct{} {
	return t.done
}

// Close shuts the shell down: closing the PTY master sends SIGHUP to the
// foreground process group; the snapshot kubeconfig directory is removed.
func (t *LocalTerminal) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	_ = t.ptmx.Close()
	select {
	case <-t.done:
	case <-time.After(3 * time.Second):
		_ = t.cmd.Process.Kill()
	}
	_ = os.RemoveAll(t.kcfgDir)
	return nil
}
