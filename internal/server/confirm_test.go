package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The page offering an expensive expansion says so in the page's language
// only, not with the English error an API client gets.
func TestConfirmPageLocalized(t *testing.T) {
	s, _ := memoServer(t)
	rec := httptest.NewRecorder()
	s.failUI(rec, httptest.NewRequest("GET", "/dailytext?unfold=3&lang=de", nil), &tooExpensiveError{level: 2, requests: 12700})
	body := rec.Body.String()
	if rec.Code != 422 || !strings.Contains(body, "Ebene 2 dieser Seite aufzuklappen braucht bis zu 12700 Abfragen") {
		t.Fatalf("status %d:\n%s", rec.Code, body)
	}
	if strings.Contains(body, "force=1 to spend them") || strings.Contains(body, "unfolding level") {
		t.Errorf("the page shows the API's English error:\n%s", body)
	}
}
