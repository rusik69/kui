package k8s

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// ForwardHandle tracks one running port-forward session.
type ForwardHandle struct {
	ID         string
	Namespace  string
	Pod        string
	LocalPort  int
	RemotePort int
	stopCh     chan struct{}
	readyCh    chan struct{}
	errCh      chan error // receives exactly one value: ForwardPorts' result
	errMu      sync.RWMutex
	termErr    error
}

// setTerminationError stores the terminal result from ForwardPorts.
func (h *ForwardHandle) setTerminationError(err error) {
	h.errMu.Lock()
	h.termErr = err
	h.errMu.Unlock()
}

// TerminationError returns the result reported when the forward stopped.
func (h *ForwardHandle) TerminationError() error {
	h.errMu.RLock()
	defer h.errMu.RUnlock()
	return h.termErr
}

// ForwardManager owns all active port-forward sessions for a process.
type ForwardManager struct {
	mu   sync.Mutex
	next int
	live map[string]*ForwardHandle
}

// ForwardSnapshot is a stable value snapshot of one active forward.
type ForwardSnapshot struct {
	ID         string
	Namespace  string
	Pod        string
	LocalPort  int
	RemotePort int
}

// NewForwardManager creates an empty manager.
func NewForwardManager() *ForwardManager {
	return &ForwardManager{live: make(map[string]*ForwardHandle)}
}

// Start begins forwarding localPort -> pod:remotePort and blocks (in a
// goroutine) until Stop is called or the forwarder errors. It returns once
// the tunnel is ready to accept connections.
func (m *ForwardManager) Start(c *Clients, namespace, pod string, localPort, remotePort int, out, errOut io.Writer) (*ForwardHandle, error) {
	transport, upgrader, err := spdy.RoundTripperFor(c.Config)
	if err != nil {
		return nil, err
	}
	url := c.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(pod).SubResource("portforward").URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)

	m.mu.Lock()
	m.next++
	id := fmt.Sprintf("pf-%d", m.next)
	m.mu.Unlock()

	h := &ForwardHandle{
		ID: id, Namespace: namespace, Pod: pod,
		LocalPort: localPort, RemotePort: remotePort,
		stopCh: make(chan struct{}), readyCh: make(chan struct{}),
		errCh: make(chan error, 1),
	}

	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, remotePort)}, h.stopCh, h.readyCh, out, errOut)
	if err != nil {
		return nil, err
	}

	go func() {
		err := fw.ForwardPorts()
		h.setTerminationError(err)
		h.errCh <- err
	}()

	select {
	case <-h.readyCh:
	case err := <-h.errCh:
		if err == nil {
			err = errors.New("port-forward tunnel ended before becoming ready")
		}
		return nil, err
	}
	if localPort == 0 {
		ports, err := fw.GetPorts()
		if err != nil {
			close(h.stopCh)
			return nil, err
		}
		h.LocalPort, err = resolvedLocalPort(localPort, ports)
		if err != nil {
			close(h.stopCh)
			return nil, err
		}
	}

	m.mu.Lock()
	m.live[id] = h
	m.mu.Unlock()

	go m.reapOnDeath(h)
	return h, nil
}

func resolvedLocalPort(requested int, ports []portforward.ForwardedPort) (int, error) {
	if requested != 0 {
		return requested, nil
	}
	if len(ports) == 0 {
		return 0, errors.New("port-forward tunnel did not report its allocated local port")
	}
	return int(ports[0].Local), nil
}

// Active returns a consistent, ID-ordered snapshot of active forwards.
func (m *ForwardManager) Active() []ForwardSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	active := make([]ForwardSnapshot, 0, len(m.live))
	for _, handle := range m.live {
		active = append(active, ForwardSnapshot{
			ID: handle.ID, Namespace: handle.Namespace, Pod: handle.Pod,
			LocalPort: handle.LocalPort, RemotePort: handle.RemotePort,
		})
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })
	return active
}

// reapOnDeath removes the handle from the live set once ForwardPorts returns,
// i.e. the tunnel is gone.
func (m *ForwardManager) reapOnDeath(h *ForwardHandle) {
	<-h.errCh
	m.mu.Lock()
	if cur, ok := m.live[h.ID]; ok && cur == h {
		delete(m.live, h.ID)
	}
	m.mu.Unlock()
}

// StopAll closes every active tunnel owned by the manager.
func (m *ForwardManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, h := range m.live {
		close(h.stopCh)
		delete(m.live, id)
	}
}

// Stop ends a running forward by id.
func (m *ForwardManager) Stop(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.live[id]
	if !ok {
		return false
	}
	close(h.stopCh)
	delete(m.live, id)
	return true
}
