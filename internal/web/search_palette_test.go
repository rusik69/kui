package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageRendersAccessibleSearchPalette(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.render(recorder, "page", PageData{ContextName: "prod", Namespace: "custom"})
	body := recorder.Body.String()
	for _, want := range []string{
		`<title>kui &middot; prod</title>`,
		`<option value="custom" selected>custom</option>`,
		`role="button" tabindex="0" aria-label="Open search"`,
		`id="namespace-filter" type="search"`,
		`id="namespace-select" name="namespace" aria-label="Namespace"`,
		`role="dialog" aria-modal="true"`,
		`aria-label="Search objects, commands and hosts" role="combobox"`,
		`hx-sync="this:replace"`,
		`role="listbox" aria-label="Search results"`,
		`role="separator" aria-label="Resize detail pane"`,
		`id="toast-container" class="toast-container" role="status" aria-live="polite"`,
		`role="dialog" aria-modal="true" aria-labelledby="confirm-message"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page HTML is missing accessible palette content %q", want)
		}
	}
}

func TestSearchResultsRenderFailureAndEmptyStates(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	failure := httptest.NewRecorder()
	server.render(failure, "searchresults", searchResults{Error: "Object search unavailable"})
	if !strings.Contains(failure.Body.String(), `role="alert"`) || strings.Contains(failure.Body.String(), "No results") {
		t.Fatalf("failure state is not distinguishable from empty results: %s", failure.Body.String())
	}

	empty := httptest.NewRecorder()
	server.render(empty, "searchresults", searchResults{})
	if !strings.Contains(empty.Body.String(), `role="status">No results</div>`) {
		t.Fatalf("empty search state missing accessible status: %s", empty.Body.String())
	}
}
