package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
)

type actMoviesStub struct {
	mgmntv1.UnimplementedMovieManagementServiceServer
	removedMovies []string
	removedFiles  []string
}

func (s *actMoviesStub) RemoveMovie(_ context.Context, req *mgmntv1.RemoveMovieRequest) (*mgmntv1.RemoveMovieResponse, error) {
	s.removedMovies = append(s.removedMovies, req.GetMovieId())
	return &mgmntv1.RemoveMovieResponse{}, nil
}

func (s *actMoviesStub) RemoveFile(_ context.Context, req *mgmntv1.RemoveFileRequest) (*mgmntv1.RemoveFileResponse, error) {
	s.removedFiles = append(s.removedFiles, req.GetFileId())
	return &mgmntv1.RemoveFileResponse{}, nil
}

type actTVStub struct {
	tvmgmtv1.UnimplementedTvManagementServiceServer
	removedSeries  []string
	removedEpisode []string
}

func (s *actTVStub) RemoveTVShow(_ context.Context, req *tvmgmtv1.RemoveTVShowRequest) (*tvmgmtv1.RemoveTVShowResponse, error) {
	s.removedSeries = append(s.removedSeries, req.GetSeriesId())
	return &tvmgmtv1.RemoveTVShowResponse{}, nil
}

func (s *actTVStub) RemoveEpisodeFile(_ context.Context, req *tvmgmtv1.RemoveEpisodeFileRequest) (*tvmgmtv1.RemoveEpisodeFileResponse, error) {
	s.removedEpisode = append(s.removedEpisode, req.GetEpisodeId())
	return &tvmgmtv1.RemoveEpisodeFileResponse{}, nil
}

func dialMoviesClient(t *testing.T, stub *actMoviesStub) mgmntv1.MovieManagementServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	mgmntv1.RegisterMovieManagementServiceServer(srv, stub)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop() })
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return mgmntv1.NewMovieManagementServiceClient(conn)
}

func dialTVClient(t *testing.T, stub *actTVStub) tvmgmtv1.TvManagementServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	tvmgmtv1.RegisterTvManagementServiceServer(srv, stub)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop() })
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return tvmgmtv1.NewTvManagementServiceClient(conn)
}

func testActModule(t *testing.T) (*Module, context.Context) {
	t.Helper()
	dir := t.TempDir()
	m := NewModule(Config{
		DBPath:   filepath.Join(dir, "maintainer.db"),
		GRPCAddr: "127.0.0.1:0",
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })
	return m, ctx
}

func insertTestCandidate(t *testing.T, db *sql.DB, c storedCandidate) {
	t.Helper()
	criteria := c.CriteriaJSON
	if criteria == "" {
		criteria = `{}`
	}
	rules := c.MatchedRuleIDs
	if rules == "" {
		rules = `[]`
	}
	_, err := db.Exec(`INSERT INTO candidates (id, scope, item_id, title, arr_action, status, collection_id, act_after, postponed_until, size_bytes, criteria_json, matched_rule_ids, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Scope, c.ItemID, c.Title, c.ArrAction, c.Status, c.CollectionID, c.ActAfter, c.PostponedUntil, c.SizeBytes, criteria, rules, nowRFC())
	if err != nil {
		t.Fatal(err)
	}
}

func insertAutoActRule(t *testing.T, db *sql.DB, id string, autoAct bool) {
	t.Helper()
	auto := 0
	if autoAct {
		auto = 1
	}
	now := nowRFC()
	_, err := db.Exec(`INSERT INTO rule_groups (id, name, enabled, scope, definition_json, outcome, arr_action, auto_act_enabled, auto_act_delay_days, created_at, updated_at)
		VALUES (?, ?, 1, 'movie', '{}', 'candidate', 'delete', ?, 0, ?, ?)`, id, id, auto, now, now)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCandidateAutoActAllowed(t *testing.T) {
	m, ctx := testActModule(t)
	insertAutoActRule(t, m.db, "rule_auto", true)
	insertAutoActRule(t, m.db, "rule_manual", false)

	tests := []struct {
		name   string
		c      storedCandidate
		global bool
		want   bool
	}{
		{
			name: "approved always acts",
			c:    storedCandidate{Status: StatusApproved, MatchedRuleIDs: `[]`},
			want: true,
		},
		{
			name: "pending without auto act blocked",
			c:    storedCandidate{Status: StatusPending, MatchedRuleIDs: `["rule_manual"]`},
			want: false,
		},
		{
			name: "pending with per-rule auto act",
			c:    storedCandidate{Status: StatusPending, MatchedRuleIDs: `["rule_auto"]`},
			want: true,
		},
		{
			name:   "pending with global auto act",
			c:      storedCandidate{Status: StatusPending, MatchedRuleIDs: `["rule_manual"]`},
			global: true,
			want:   true,
		},
		{
			name: "postponed expired resumes",
			c: storedCandidate{
				Status:         StatusPostponed,
				PostponedUntil: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
				MatchedRuleIDs: `[]`,
			},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m.cfgMu.Lock()
			m.autoActEnabled = tc.global
			m.cfgMu.Unlock()
			if got := m.candidateAutoActAllowed(ctx, tc.c); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestLoadActionableCandidatesPendingGate(t *testing.T) {
	m, ctx := testActModule(t)
	insertAutoActRule(t, m.db, "rule_auto", false)
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	insertTestCandidate(t, m.db, storedCandidate{
		ID: "c_pending", Scope: ScopeMovie, ItemID: "m1", Title: "Pending",
		ArrAction: ActionDelete, Status: StatusPending, ActAfter: past,
		MatchedRuleIDs: `["rule_auto"]`, CriteriaJSON: `{}`,
	})
	insertTestCandidate(t, m.db, storedCandidate{
		ID: "c_approved", Scope: ScopeMovie, ItemID: "m2", Title: "Approved",
		ArrAction: ActionDelete, Status: StatusApproved, ActAfter: past,
		MatchedRuleIDs: `[]`, CriteriaJSON: `{}`,
	})
	got := m.loadActionableCandidates(ctx, 10)
	if len(got) != 1 || got[0].ID != "c_approved" {
		t.Fatalf("expected only approved candidate, got %+v", got)
	}
}

func TestRunActDryRunNoDelete(t *testing.T) {
	moviesStub := &actMoviesStub{}
	m, ctx := testActModule(t)
	m.moviesClient = dialMoviesClient(t, moviesStub)
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	insertTestCandidate(t, m.db, storedCandidate{
		ID: "c1", Scope: ScopeMovie, ItemID: "movie-1", Title: "Dry",
		ArrAction: ActionDelete, Status: StatusApproved, ActAfter: past,
		CriteriaJSON: `{}`,
	})
	taken, failed, err := m.runAct(ctx, actOptions{dryRun: true, maxActions: 5})
	if err != nil {
		t.Fatal(err)
	}
	if taken != 1 || failed != 0 {
		t.Fatalf("taken=%d failed=%d", taken, failed)
	}
	if len(moviesStub.removedMovies) != 0 {
		t.Fatalf("dry-run should not delete, removed=%v", moviesStub.removedMovies)
	}
}

func TestRunActDiskGateBlocks(t *testing.T) {
	moviesStub := &actMoviesStub{}
	m, ctx := testActModule(t)
	m.moviesClient = dialMoviesClient(t, moviesStub)
	m.diskActMaxFreePercent = 0.01
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	criteria, _ := json.Marshal(EvalContext{RootFolderPath: "/"})
	insertTestCandidate(t, m.db, storedCandidate{
		ID: "c1", Scope: ScopeMovie, ItemID: "movie-1", Title: "Blocked",
		ArrAction: ActionDelete, Status: StatusApproved, ActAfter: past,
		CriteriaJSON: string(criteria),
	})
	taken, _, err := m.runAct(ctx, actOptions{maxActions: 5})
	if err != nil {
		t.Fatal(err)
	}
	if taken != 0 {
		t.Fatalf("disk gate should block, taken=%d removed=%v", taken, moviesStub.removedMovies)
	}
}

func TestLoadFreeUpCandidatesSizeOrder(t *testing.T) {
	m, ctx := testActModule(t)
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	insertTestCandidate(t, m.db, storedCandidate{
		ID: "small", Scope: ScopeMovie, ItemID: "s", Title: "Small",
		ArrAction: ActionDelete, Status: StatusApproved, ActAfter: past, SizeBytes: 100,
		CriteriaJSON: `{}`,
	})
	insertTestCandidate(t, m.db, storedCandidate{
		ID: "large", Scope: ScopeMovie, ItemID: "l", Title: "Large",
		ArrAction: ActionDelete, Status: StatusApproved, ActAfter: past, SizeBytes: 999999,
		CriteriaJSON: `{}`,
	})
	got := m.loadFreeUpCandidates(ctx, 10)
	if len(got) < 2 {
		t.Fatalf("expected 2 candidates, got %d", len(got))
	}
	if got[0].ID != "large" {
		t.Fatalf("expected largest first, got %s", got[0].ID)
	}
}

func TestDeleteItemRemoveMovieAndTVShow(t *testing.T) {
	moviesStub := &actMoviesStub{}
	tvStub := &actTVStub{}
	m, ctx := testActModule(t)
	m.moviesClient = dialMoviesClient(t, moviesStub)
	m.tvClient = dialTVClient(t, tvStub)

	if err := m.deleteItem(ctx, storedCandidate{Scope: ScopeMovie, ItemID: "mv-42", ArrAction: ActionDelete}); err != nil {
		t.Fatal(err)
	}
	if len(moviesStub.removedMovies) != 1 || moviesStub.removedMovies[0] != "mv-42" {
		t.Fatalf("RemoveMovie: %+v", moviesStub.removedMovies)
	}

	if err := m.deleteItem(ctx, storedCandidate{Scope: ScopeSeries, ItemID: "tv-7", ArrAction: ActionDelete}); err != nil {
		t.Fatal(err)
	}
	if len(tvStub.removedSeries) != 1 || tvStub.removedSeries[0] != "tv-7" {
		t.Fatalf("RemoveTVShow: %+v", tvStub.removedSeries)
	}
}

func TestSettingsPersistRoundTrip(t *testing.T) {
	m, ctx := testActModule(t)
	if err := m.updateSetting("dry_run", "true"); err != nil {
		t.Fatal(err)
	}
	m2 := NewModule(Config{DBPath: m.dbPath, GRPCAddr: "127.0.0.1:0"})
	if err := m2.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer m2.Stop(ctx)
	if !m2.getDryRun() {
		t.Fatal("expected dry_run persisted across init")
	}
}
