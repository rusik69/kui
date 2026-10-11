package web

import (
	"encoding/json"
	"net/http"
)

// triggerToast asks the browser to show a dismissing toast notification via
// htmx's HX-Trigger mechanism. Must be called before the response body is
// written (it sets a header).
func triggerToast(w http.ResponseWriter, level, message string) {
	payload, err := json.Marshal(map[string]any{
		"toast": map[string]string{"level": level, "message": message},
	})
	if err != nil {
		return
	}
	w.Header().Set("HX-Trigger", string(payload))
}
