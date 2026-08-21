package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseJustWatchPolicy(t *testing.T) {
	raw := `{"country":"US","language":"en","mode":"available_on","providers":["netflix"]}`
	p, err := parseJustWatchPolicy("id1", "Netflix keep", raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Country != "US" || p.Mode != "available_on" || len(p.Providers) != 1 {
		t.Fatalf("got %+v", p)
	}
}

func TestJustWatchAvailableOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"popularTitles": map[string]any{
					"edges": []any{
						map[string]any{
							"node": map[string]any{
								"objectType": "MOVIE",
								"content": map[string]any{
									"title":               "Test Movie",
									"originalReleaseYear": 2020,
									"offers": []any{
										map[string]any{"package": map[string]any{"technicalName": "netflix"}},
									},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("JUSTWATCH_API_URL", srv.URL)
	jw := newJustWatchClient("US", "en", srv.Client())
	ok := jw.availableOn(context.Background(), "Test Movie", 2020, "movie", []string{"netflix"})
	if !ok {
		t.Fatal("expected available on netflix")
	}
}

func TestIsImportExcluded(t *testing.T) {
	m := NewModule(Config{})
	excluded := map[string]struct{}{"movie:42": {}}
	if !m.isImportExcluded(ScopeMovie, 42, excluded) {
		t.Fatal("expected import exclusion")
	}
}
