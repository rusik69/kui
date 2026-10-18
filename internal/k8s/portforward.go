package k8s

import (
	"errors"
	"fmt"
	"io"
	"net/http"
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
}

// ForwardManager owns all active port-forward sessions for a process.
type ForwardManager struct {
	mu   sync.Mutex
	next int
	live map[string]*ForwardHandle
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

	go func() { h.errCh <- fw.ForwardPorts() }()

	select {
	case <-h.readyCh:
	case err := <-h.errCh:
		// The tunnel died before it was ever ready (pod unreachable, API
		// rejected the request, ...). ForwardPorts may report that as a nil
		// error, which must not be read as "all is well".
		if err == nil {
			err = errors.New("port-forward tunnel ended before becoming ready")
		}
		return nil, err
	}

	m.mu.Lock()
	m.live[id] = h
	m.mu.Unlock()

	// Reap the handle if the forwarder dies on its own (the pod restarts, the
	// API connection drops, ...). Without this a dead tunnel stays in the
	// live map forever, and its Stop button keeps claiming a forward exists.
	go m.reapOnDeath(h)

	return h, nil
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
