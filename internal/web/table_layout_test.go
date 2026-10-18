package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestEmptyTableColspanIncludesOptionalHeaders(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := TableData{
		Kind:              "Pod",
		Columns:           []string{"Name", "Age"},
		ShowNamespace:     true,
		Selectable:        true,
		HeaderColumnCount: tableHeaderColumnCount([]string{"Name", "Age"}, true, true),
	}
	recorder := httptest.NewRecorder()
	server.render(recorder, "table", data)
	body := recorder.Body.String()
	if !strings.Contains(body, `colspan="4"`) {
		t.Fatalf("empty table HTML does not span all 4 headers: %s", body)
	}
	if !strings.Contains(body, `data-filter-empty`) || !strings.Contains(body, `No resources match this filter`) {
		t.Fatalf("empty table HTML is missing the filter no-results row: %s", body)
	}
}

func TestOverviewBarsUseRelativeCounts(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := OverviewData{ObjectCounts: []ObjectCount{
		{Kind: "Pods", Count: 8, BarPercent: 100},
		{Kind: "Services", Count: 4, BarPercent: 50},
		{Kind: "Secrets", Count: 0, BarPercent: 0},
	}}
	recorder := httptest.NewRecorder()
	server.render(recorder, "overview", data)
	body := recorder.Body.String()
	for _, width := range []string{`width:100%`, `width:50%`, `width:0%`} {
		if !strings.Contains(body, width) {
			t.Errorf("overview HTML is missing relative bar width %q", width)
		}
	}
}

func TestResourceListURLCarriesSelectorsAndCursor(t *testing.T) {
	raw := resourceListURL("/c/context/default/pods", "app=web", "metadata.name=web-2", 500, "next-token")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("frame") != "table" || query.Get("labelSelector") != "app=web" || query.Get("fieldSelector") != "metadata.name=web-2" || query.Get("limit") != "500" || query.Get("continue") != "next-token" {
		t.Fatalf("resourceListURL query = %v, selectors or pagination state missing", query)
	}
}

func TestParseResourceListOptions(t *testing.T) {
	tests := []struct {
		name    string
		query   url.Values
		limit   int64
		wantErr bool
	}{
		{name: "default", query: url.Values{}, limit: defaultResourcePageSize},
		{name: "valid", query: url.Values{"labelSelector": {"app=web"}, "fieldSelector": {"metadata.name=web-2"}, "limit": {"500"}, "continue": {"cursor"}}, limit: 500},
		{name: "not a number", query: url.Values{"limit": {"many"}}, wantErr: true},
		{name: "zero", query: url.Values{"limit": {"0"}}, wantErr: true},
		{name: "over maximum", query: url.Values{"limit": {"1001"}}, wantErr: true},
		{name: "invalid label", query: url.Values{"labelSelector": {"="}}, wantErr: true},
		{name: "invalid field", query: url.Values{"fieldSelector": {"metadata.name in ("}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options, err := parseResourceListOptions(tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseResourceListOptions error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && options.Limit != tt.limit {
				t.Errorf("limit = %d, want %d", options.Limit, tt.limit)
			}
			if err == nil && tt.name == "valid" && (options.LabelSelector != "app=web" || options.FieldSelector != "metadata.name=web-2" || options.Continue != "cursor") {
				t.Errorf("selector/cursor options = %+v", options)
			}
		})
	}
}

func TestTableRendersSelectorControls(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.render(recorder, "table", TableData{
		Kind: "Pod", Columns: []string{"Name"}, ShowNamespace: true,
		Rows:          []Row{{Name: "pod-a", Namespace: "default", Cells: []string{"pod-a"}, DetailURL: "/c/context/default/pods/pod-a"}},
		LabelSelector: "app=web", FieldSelector: "metadata.name=web-2",
		Selectable: true, PageSize: 200, NextPageURL: "/c/context/default/pods?continue=next",
	})
	body := recorder.Body.String()
	for _, want := range []string{
		`id="label-selector" value="app=web"`,
		`<th scope="col" tabindex="0" aria-sort="none">Name</th>`,
		`id="field-selector" value="metadata.name=web-2"`,
		`id="page-size" min="1" max="1000" value="200"`,
		`id="row-density"`,
		`value="compact">Compact`,
		`data-next-url="/c/context/default/pods?continue=next"`,
		`kui.applyResourceSelectors()`,
		`kui.setRowDensity(this.value)`,
		`data-copy-text="pod-a" aria-label="Copy resource name"`,
		`data-copy-text="default" aria-label="Copy namespace"`,
		`kui.copyText(this.dataset.copyText)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("table HTML is missing selector/pagination content %q", want)
		}
	}
}

func TestTableHidesNextPageWithoutContinuation(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.render(recorder, "table", TableData{Selectable: true, PageSize: defaultResourcePageSize})
	if strings.Contains(recorder.Body.String(), `data-next-url=`) {
		t.Fatal("table exposed a next-page action without a continuation token")
	}
}
