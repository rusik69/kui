package web

import (
	"encoding/base64"

	"github.com/rusik69/kui/internal/k8s"
)

// Kubeconfig context names (especially EKS ARNs like
// "arn:aws:eks:us-east-1:123:cluster/name") contain ":" and "/", which break
// the single-segment {ctx} URL wildcard. Every URL carries the base64url-encoded
// id instead; decodeCtx recovers the real name only where it's needed (talking
// to the Manager or displaying it), never when building further URLs.
func encodeCtx(name string) string { return base64.RawURLEncoding.EncodeToString([]byte(name)) }

func decodeCtx(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// clients resolves the client bundle for the context id as it appears in a URL.
func (s *Server) clients(encCtx string) (*k8s.Clients, error) {
	name, err := decodeCtx(encCtx)
	if err != nil {
		return nil, err
	}
	return s.mgr.Clients(name)
}
