package web

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type templateExecutionFailure struct{}

func (templateExecutionFailure) Fail() (string, error) {
	return "", errors.New("template field failed")
}

func TestRenderDoesNotWritePartialTemplateOnExecutionError(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server.tmpl, err = template.New("partial").Parse("partial output{{.Fail}}")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	server.render(rec, "partial", templateExecutionFailure{})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "partial output") {
		t.Fatalf("partial template output escaped before the error: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "template field failed") {
		t.Fatalf("error response missing execution failure: %q", rec.Body.String())
	}
}
