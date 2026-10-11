package k8s

import (
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
	}

	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, remotePort)}, h.stopCh, h.readyCh, out, errOut)
	if err != nil {
		return nil, err
	}

	errCh := make(chan error, 1)
	go func() { errCh <- fw.ForwardPorts() }()

	select {
	case <-h.readyCh:
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	m.live[id] = h
	m.mu.Unlock()
	return h, nil
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

// List returns every currently active forward.
func (m *ForwardManager) List() []*ForwardHandle {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*ForwardHandle, 0, len(m.live))
	for _, h := range m.live {
		out = append(out, h)
	}
	return out
}
