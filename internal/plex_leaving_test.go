package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newTestModule(t *testing.T) *Module {
	t.Helper()
	dir := t.TempDir()
	m := NewModule(Config{
		DBPath:   filepath.Join(dir, "maintainer.db"),
		GRPCAddr: "127.0.0.1:0",
	})
	if err := m.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return m
}

func TestPlexLeavingSoonCreateCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"abc123"}}`))
		case r.URL.Path == "/library/sections":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","type":"movie","title":"Movies"}]}}`))
		case strings.HasPrefix(r.URL.Path, "/library/sections/1/all"):
			if r.URL.Query().Get("guid") == "tmdb://603" {
				_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[{"ratingKey":"100"}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[]}}`))
		case r.URL.Path == "/library/collections" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"Metadata":[{"ratingKey":"200"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("PLEX_URL", srv.URL)
	t.Setenv("PLEX_TOKEN", "test-token")

	m := newTestModule(t)
	col := &storedCollection{
		ID:                 "col-1",
		LeavingSoonEnabled: true,
		LeavingSoonLabel:   "Leaving Soon",
	}
	ec := EvalContext{
		Scope:  ScopeMovie,
		Title:  "The Matrix",
		TmdbID: 603,
	}
	m.syncLeavingSoonToPlex(context.Background(), ec, col)
	if col.PlexCollectionKey != "200" {
		t.Fatalf("expected plex collection key 200, got %q", col.PlexCollectionKey)
	}
}

func TestPlexLeavingSoonAddToExistingCollection(t *testing.T) {
	addCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"abc123"}}`))
		case r.URL.Path == "/library/sections":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","type":"movie","title":"Movies"}]}}`))
		case strings.HasPrefix(r.URL.Path, "/library/sections/1/all"):
			_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[{"ratingKey":"100"}]}}`))
		case strings.HasPrefix(r.URL.Path, "/library/collections/200/items") && r.Method == http.MethodPut:
			addCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("PLEX_URL", srv.URL)
	t.Setenv("PLEX_TOKEN", "test-token")

	m := newTestModule(t)
	col := &storedCollection{
		ID:                 "col-1",
		LeavingSoonEnabled: true,
		LeavingSoonLabel:   "Leaving Soon",
		PlexCollectionKey:  "200",
	}
	ec := EvalContext{Scope: ScopeMovie, TmdbID: 603}
	m.syncLeavingSoonToPlex(context.Background(), ec, col)
	if !addCalled {
		t.Fatal("expected add to collection call")
	}
}

func TestPlexClientMachineIdentifier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "tok" {
			t.Fatalf("missing token header")
		}
		_, _ = w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"mid"}}`))
	}))
	defer srv.Close()

	p := &plexClient{baseURL: srv.URL, token: "tok", cli: srv.Client()}
	id, err := p.machineIdentifier(context.Background())
	if err != nil || id != "mid" {
		t.Fatalf("machine id: %v %q", err, id)
	}
}

func TestPlexClientNoConfig(t *testing.T) {
	t.Setenv("PLEX_URL", "")
	t.Setenv("PLEX_TOKEN", "")
	m := newTestModule(t)
	if m.plexClient() != nil {
		t.Fatal("expected nil plex client without env")
	}
}

func TestPlexRemoveFromCollection(t *testing.T) {
	removed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/":
			_, _ = w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"abc123"}}`))
		case r.URL.Path == "/library/sections":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","type":"movie","title":"Movies"}]}}`))
		case strings.HasPrefix(r.URL.Path, "/library/sections/1/all") && r.URL.Query().Get("guid") != "":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[{"ratingKey":"100"}]}}`))
		case strings.HasPrefix(r.URL.Path, "/library/collections/200/items") && r.Method == http.MethodDelete:
			removed = true
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("PLEX_URL", srv.URL)
	t.Setenv("PLEX_TOKEN", "test-token")

	m := newTestModule(t)
	m.removeLeavingSoonFromPlex(context.Background(), EvalContext{Scope: ScopeMovie, TmdbID: 603}, "200", "Leaving Soon")
	if !removed {
		t.Fatal("expected remove from collection")
	}
}

func TestPlexSectionEnvOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/library/sections" {
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[
				{"key":"1","type":"movie","title":"Movies A"},
				{"key":"2","type":"movie","title":"Movies B"}
			]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("MAINTAINER_PLEX_MOVIE_SECTION", "Movies B")
	p := &plexClient{baseURL: srv.URL, token: "tok", cli: srv.Client(), sectionKeys: make(map[string]string)}
	key, err := p.sectionKey(context.Background(), ScopeMovie)
	if err != nil || key != "2" {
		t.Fatalf("section key: %v %q", err, key)
	}
}
