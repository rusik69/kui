package web

import (
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctxName := r.PathValue("ctx")
	ns := r.URL.Query().Get("ns")

	c, err := s.clients(ctxName)
	if err != nil {
		fmt.Fprint(w, "Unreachable")
		return
	}
	// ResourceVersion "0": this endpoint polls every 3s, so the list is
	// served from the API server's watch cache (possibly seconds stale) rather
	// than reading etcd each time — right for a status indicator.
	pods, err := c.Clientset.CoreV1().Pods(nsOrAll(ns)).List(r.Context(), metav1.ListOptions{ResourceVersion: "0"})
	if err != nil {
		fmt.Fprint(w, "Unreachable")
		return
	}
	tally := tallyPodPhases(pods.Items)
	fmt.Fprintf(w, "Live &middot; %d running &middot; %d failing &middot; every 3s", tally.Running, tally.Unhealthy)
}
