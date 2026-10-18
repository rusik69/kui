package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsHandlersReturnContextSetupErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "pods", handler: (&Server{}).handleMetricsPods},
		{name: "nodes", handler: (&Server{}).handleMetricsNodes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			req.SetPathValue("ctx", "invalid-base64!")
			recorder := httptest.NewRecorder()
			tt.handler(recorder, req)
			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
			}
			if recorder.Body.Len() == 0 {
				t.Fatal("metrics setup failure returned an empty response")
			}
		})
	}
}
