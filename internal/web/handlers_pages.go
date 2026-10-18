package web

import (
	"context"
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rusik69/kui/internal/k8s"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	ctxName := s.defaultContext
	if ctxName == "" {
		var err error
		ctxName, err = s.mgr.DefaultContext()
		if err != nil {
			http.Error(w, "unable to load kubeconfig context", http.StatusServiceUnavailable)
			return
		}
	}
	if ctxName == "" {
		var err error
		ctxName, err = s.fallbackContext()
		if err != nil {
			http.Error(w, "no usable kubeconfig context is configured", http.StatusServiceUnavailable)
			return
		}
	}
	http.Redirect(w, r, "/c/"+encodeCtx(ctxName), http.StatusFound)
}

func (s *Server) handleContextPage(w http.ResponseWriter, r *http.Request) {
	ctxID := r.PathValue("ctx")
	data, err := s.buildPageData(r.Context(), ctxID, "", "overview")
	if err != nil {
		fallback, fallbackErr := s.fallbackContext()
		if fallbackErr == nil && fallback != "" && encodeCtx(fallback) != ctxID {
			http.Redirect(w, r, "/c/"+encodeCtx(fallback), http.StatusFound)
			return
		}
		http.Error(w, "unable to load kubeconfig context: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.render(w, "page", data)
}

func (s *Server) fallbackContext() (string, error) {
	contexts, err := s.mgr.ListContexts()
	if err != nil {
		return "", err
	}
	contains := func(name string) bool {
		for _, context := range contexts {
			if context.Name == name {
				return true
			}
		}
		return false
	}
	if contains(s.defaultContext) {
		return s.defaultContext, nil
	}
	if name, err := s.mgr.DefaultContext(); err == nil && contains(name) {
		return name, nil
	}
	if len(contexts) > 0 {
		return contexts[0].Name, nil
	}
	return "", fmt.Errorf("kubeconfig has no contexts")
}

func defaultNamespace(contexts []k8s.ContextInfo, ctxName string) string {
	for _, ctx := range contexts {
		if ctx.Name == ctxName && ctx.Namespace != "" {
			return ctx.Namespace
		}
	}
	return "_all"
}

// buildPageData takes ctxID as it appears in the URL (base64url-encoded,
// see ctxid.go) and resolves the real context name only for display and
// cluster calls — every URL field below reuses ctxID verbatim.
//
// Cluster connectivity (expired SSO, unreachable API server, etc.) must
// never take down the page shell: the sidebar, context switcher, and
// namespace selector are the user's only way to escape a broken context, so
// a failure building the live client or listing namespaces is recorded in
// ConnError and surfaced inline instead of failing this function. Only a
// genuinely unreadable kubeconfig (decodeCtx/ListContexts) is fatal — there's
// no usable shell to render at all in that case.
func (s *Server) buildPageData(ctx context.Context, ctxID, ns, resource string) (PageData, error) {
	ctxName, err := decodeCtx(ctxID)
	if err != nil {
		return PageData{}, err
	}
	rawContexts, err := s.mgr.ListContexts()
	if err != nil {
		return PageData{}, err
	}

	contextFound := false
	for _, item := range rawContexts {
		if item.Name == ctxName {
			contextFound = true
			break
		}
	}
	if !contextFound {
		return PageData{}, fmt.Errorf("context %q is not present in kubeconfig", ctxName)
	}
	if ns == "" {
		if ctxName == s.defaultContext && s.defaultNamespace != "" {
			ns = s.defaultNamespace
		} else {
			ns = defaultNamespace(rawContexts, ctxName)
		}
	}
	contexts := make([]ContextOption, 0, len(rawContexts))
	for _, rc := range rawContexts {
		contexts = append(contexts, ContextOption{ID: encodeCtx(rc.Name), Name: rc.Name, Current: rc.Name == ctxName})
	}

	var namespaces []string
	var connErr string
	c, err := s.clients(ctxID)
	if err != nil {
		connErr = err.Error()
	} else if namespaces, err = listNamespaceNames(ctx, c); err != nil {
		connErr = err.Error()
	}

	groupKinds := map[string][]k8s.ResourceKind{}
	for _, g := range k8s.Groups() {
		groupKinds[g] = k8s.InGroup(g)
	}

	return PageData{
		Context:       ctxID,
		ContextName:   ctxName,
		Contexts:      contexts,
		Namespace:     ns,
		Namespaces:    namespaces,
		Resource:      resource,
		Groups:        k8s.Groups(),
		GroupKinds:    groupKinds,
		ListURL:       "/c/" + ctxID + "/" + ns + "/" + resource + "?frame=table",
		WatchURL:      "/c/" + ctxID + "/" + ns + "/" + resource + "/watch",
		BulkDeleteURL: "/c/" + ctxID + "/bulk-delete",
		CSRFToken:     s.csrfToken,
		ConnError:     connErr,
	}, nil
}

func listNamespaceNames(ctx context.Context, c *k8s.Clients) ([]string, error) {
	list, err := c.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list.Items))
	for _, ns := range list.Items {
		out = append(out, ns.Name)
	}
	return out, nil
}
