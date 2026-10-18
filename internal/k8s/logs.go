package k8s

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
)

// LogOptions controls a pod log stream.
type LogOptions struct {
	Container  string
	Follow     bool
	Previous   bool
	Timestamps bool
	TailLines  *int64
	LimitBytes *int64
}

// StreamLogs opens a log stream for one container and returns the
// ReadCloser the caller should copy from (and must Close when done).
func StreamLogs(ctx context.Context, c *Clients, namespace, pod string, opts LogOptions) (io.ReadCloser, error) {
	req := c.Clientset.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container:  opts.Container,
		Follow:     opts.Follow,
		Previous:   opts.Previous,
		Timestamps: opts.Timestamps,
		TailLines:  opts.TailLines,
		LimitBytes: opts.LimitBytes,
	})
	return req.Stream(ctx)
}
