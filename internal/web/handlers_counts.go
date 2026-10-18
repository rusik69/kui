package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rusik69/kui/internal/k8s"
)

// ObjectCount is one resource kind's tally — used by both the sidebar-badge
// endpoint and the Overview Objects panel, so the two never disagree.
type ObjectCount struct {
	Name       string `json:"-"` // registry short name, e.g. "pods" — join key for data-count-for, used as the map key not a field
	Kind       string `json:"kind"`
	Count      int    `json:"count"`
	Failing    bool   `json:"failing"` // true only for pods with at least one Failed-phase object
	BarPercent int    `json:"-"`
}

const fanOutTimeout = 5 * time.Second

// countsFromFanOut turns FanOutList's raw per-kind results into per-kind
// counts, skipping kinds the current token can't list (badge/row just stays
// blank for those rather than erroring the whole page).
func countsFromFanOut(results []k8s.KindResult) []ObjectCount {
	out := make([]ObjectCount, 0, len(results))
	for _, res := range results {
		if res.Err != nil {
			continue
		}
		oc := ObjectCount{Name: res.Kind.Name, Kind: res.Kind.Kind, Count: len(res.List.Items)}
		if res.Kind.Name == "pods" {
			for i := range res.List.Items {
				if podStateIsBad(podStateFromUnstructured(&res.List.Items[i])) {
					oc.Failing = true
					break
				}
			}
		}
		out = append(out, oc)
	}

	maxCount := 0
	for _, oc := range out {
		if oc.Count > maxCount {
			maxCount = oc.Count
		}
	}
	if maxCount > 0 {
		for i := range out {
			out[i].BarPercent = out[i].Count * 100 / maxCount
		}
	}
	return out
}

func (s *Server) handleCounts(w http.ResponseWriter, r *http.Request) {
	ctxName := r.PathValue("ctx")
	ns := r.URL.Query().Get("ns")
	c, err := s.clients(ctxName)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), fanOutTimeout)
	defer cancel()
	counts := countsFromFanOut(k8s.FanOutList(ctx, c, ns))

	byName := make(map[string]ObjectCount, len(counts))
	for _, oc := range counts {
		byName[oc.Name] = oc
	}
	payload, err := json.Marshal(byName)
	if err != nil {
		return
	}
	fmt.Fprintf(w, `<script>kui.applyCounts(%s)</script>`, payload)
}
