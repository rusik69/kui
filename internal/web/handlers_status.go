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
	pods, err := c.Clientset.CoreV1().Pods(nsOrAll(ns)).List(r.Context(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprint(w, "Unreachable")
		return
	}
	tally := tallyPodPhases(pods.Items)
	fmt.Fprintf(w, "Live &middot; %d running &middot; %d failing &middot; every 3s", tally.Running, tally.Unhealthy)
}
