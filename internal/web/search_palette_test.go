package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestPageExposesAccessibleNavigationAndStatusRegions(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.render(recorder, "page", PageData{ContextName: "prod", Namespace: "custom"})
	root, err := html.Parse(strings.NewReader(recorder.Body.String()))
	if err != nil {
		t.Fatal(err)
	}

	search := findHTMLNode(root, func(node *html.Node) bool {
		return node.Data == "button" && hasHTMLAttr(node, "aria-keyshortcuts", "Control+K Meta+K")
	})
	if search == nil {
		t.Fatal("search palette trigger is not a keyboard-accessible native button")
	}
	if findHTMLNode(root, func(node *html.Node) bool {
		return node.Data == "main" && hasHTMLAttr(node, "id", "main-content")
	}) == nil {
		t.Fatal("page has no primary main landmark")
	}
	if findHTMLNode(root, func(node *html.Node) bool {
		return node.Data == "nav" && hasHTMLAttr(node, "aria-label", "Resource navigation")
	}) == nil {
		t.Fatal("resource navigation landmark is missing")
	}
	if countHTMLID(root, "toast-container") != 1 {
		t.Fatal("toast announcements must have exactly one live region")
	}
	if findHTMLNode(root, func(node *html.Node) bool {
		return hasHTMLAttr(node, "id", "watch-state-label") && hasHTMLAttr(node, "aria-live", "polite")
	}) == nil {
		t.Fatal("watch connection state is not exposed through a live status region")
	}
}

func TestSearchResultStatesExposeDistinctAccessibleRoles(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	failure := httptest.NewRecorder()
	server.render(failure, "searchresults", searchResults{Error: "Object search unavailable"})
	failureRoot, err := html.Parse(strings.NewReader(failure.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	if findHTMLNode(failureRoot, func(node *html.Node) bool {
		return hasHTMLAttr(node, "role", "alert")
	}) == nil {
		t.Fatal("search failure is not exposed as an alert")
	}
	if findHTMLNode(failureRoot, func(node *html.Node) bool {
		return hasHTMLAttr(node, "class", "palette-empty")
	}) != nil {
		t.Fatal("search failure is incorrectly exposed as an empty result")
	}

	empty := httptest.NewRecorder()
	server.render(empty, "searchresults", searchResults{})
	emptyRoot, err := html.Parse(strings.NewReader(empty.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	if findHTMLNode(emptyRoot, func(node *html.Node) bool {
		return node.Data == "div" && hasHTMLAttr(node, "class", "palette-empty") && hasHTMLAttr(node, "role", "status")
	}) == nil {
		t.Fatal("empty search results are not exposed as a status")
	}
}

func findHTMLNode(node *html.Node, match func(*html.Node) bool) *html.Node {
	if match(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findHTMLNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func countHTMLID(node *html.Node, id string) int {
	count := 0
	if hasHTMLAttr(node, "id", id) {
		count++
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		count += countHTMLID(child, id)
	}
	return count
}

func hasHTMLAttr(node *html.Node, key, value string) bool {
	if node == nil {
		return false
	}
	for _, attr := range node.Attr {
		if attr.Key == key && (value == "" || attr.Val == value || key == "class" && hasClassToken(attr.Val, value)) {
			return true
		}
	}
	return false
}

func hasClassToken(classes, token string) bool {
	for _, class := range strings.Fields(classes) {
		if class == token {
			return true
		}
	}
	return false
}
