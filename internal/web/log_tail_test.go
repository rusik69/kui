package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLogsValidatesTailLinesBeforeWebSocketUpgrade(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  int
	}{
		{value: "0", want: http.StatusBadRequest},
		{value: "5001", want: http.StatusBadRequest},
		{value: "invalid", want: http.StatusBadRequest},
		{value: "1", want: http.StatusBadGateway},
		{value: "5000", want: http.StatusBadGateway},
	} {
		t.Run(tt.value, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?tailLines="+tt.value, nil)
			req.SetPathValue("ctx", "!")
			recorder := httptest.NewRecorder()
			(&Server{}).handleLogs(recorder, req)
			if recorder.Code != tt.want {
				t.Errorf("status = %d, want %d; body: %s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}
