package internal

import (
	"context"
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"google.golang.org/grpc"

	authv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/auth/v1"
	discoveryv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/discovery/v1"
	"github.com/Muxcore-Media/core/sdk/go/module/erasure"
	"github.com/Muxcore-Media/core/sdk/go/module/erasure/erasuretest"
	"github.com/Muxcore-Media/core/sdk/go/module/meshtls"
)

const (
	testProviderID = "auth-local"
	testOwnerID    = "media-library-maintainer"

	// Ids are 128-bit random hex in auth-local; these are shaped like them.
	victimID    = "7f3a9c1e0b2d4a68"
	bystanderID = "c2d4e6f8a0b1c3d5"
)

func clearMeshEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"MUXCORE_INSECURE_DISABLE_TLS", "MUXCORE_DEV_TLS_SKIP", "MUXCORE_GRPC_INSECURE",
		meshtls.EnvTLSCert, meshtls.EnvTLSKey, meshtls.EnvTLSCA, meshtls.EnvTLSServerName,
		"MUXCORE_PROFILE", "MUXCORE_MESH_DIAL_LOCAL", "MUXCORE_GRPC_ADDR", erasure.EnvSweepInterval} {
		t.Setenv(k, "")
	}
}

func devEnv(t *testing.T) {
	t.Helper()
	clearMeshEnv(t)
	t.Setenv("MUXCORE_PROFILE", "dev")
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "true")
}

func fastTune(c *erasure.Config) {
	c.AckBackoff = time.Millisecond
	c.RetryBackoff = 50 * time.Millisecond
	c.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	c.CallTimeout = 5 * time.Second
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// newErasureTestModule initialises a module on dbPath without a core connection. It
// is stopped at cleanup.
func newErasureTestModule(t *testing.T, dbPath string, tune func(*Config)) *Module {
	t.Helper()
	cfg := Config{DBPath: dbPath, GRPCAddr: "127.0.0.1:0", ErasureTune: fastTune}
	if tune != nil {
		tune(&cfg)
	}
	m := NewModule(cfg)
	if err := m.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return m
}

type seedCandidate struct {
	id, item, criteria string
}

// seedCandidates inserts candidates with the given criteria_json documents.
func seedCandidates(t *testing.T, m *Module, rows []seedCandidate) {
	t.Helper()
	for _, r := range rows {
		_, err := m.db.ExecContext(context.Background(), `INSERT INTO candidates
			(id, scope, item_id, title, matched_rule_ids, arr_action, status, added_at, act_after, criteria_json)
			VALUES (?, 'movie', ?, ?, '[]', 'delete', 'pending', '2026-10-01T00:00:00Z', '2026-10-08T00:00:00Z', ?)`,
			r.id, r.item, "title "+r.id, r.criteria)
		if err != nil {
			t.Fatalf("seed %s: %v", r.id, err)
		}
	}
}

// mixedCandidates is the shared corpus: three candidates that name the victim
// (as requester, in the percent map, in the duration map), one that names
// only the bystander, and decoys that merely resemble the victim.
func mixedCandidates() []seedCandidate {
	return []seedCandidate{
		{"cand_req", "m1", `{"RequestedBy":"` + victimID + `","Requested":true,"UserWatchedPercent":{"` + bystanderID + `":100}}`},
		{"cand_pct", "m2", `{"RequestedBy":"` + bystanderID + `","UserWatchedPercent":{"` + victimID + `":50,"` + bystanderID + `":10}}`},
		{"cand_dur", "m3", `{"UserWatchedDurationMinutes":{"` + victimID + `":42.5}}`},
		{"cand_bystander", "m4", `{"RequestedBy":"` + bystanderID + `","UserWatchedPercent":{"` + bystanderID + `":0}}`},
		{"cand_none", "m5", `{}`},
		{"cand_prefix_longer", "m6", `{"RequestedBy":"` + victimID + `0","UserWatchedPercent":{"` + victimID + `0":1}}`},
		{"cand_prefix_shorter", "m7", `{"RequestedBy":"` + victimID[:8] + `","UserWatchedDurationMinutes":{"` + victimID[:8] + `":1}}`},
		{"cand_username", "m8", `{"RequestedBy":"alice","UserWatchedPercent":{"alice":100}}`},
		{"cand_other_case", "m9", `{"RequestedBy":"` + "7F3A9C1E0B2D4A68" + `"}`},
		{"cand_title_only", "m10", `{"Title":"` + victimID + `","ItemID":"` + victimID + `"}`},
	}
}

var victimCandidateIDs = []string{"cand_req", "cand_pct", "cand_dur"}

func candidateIDs(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM candidates ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func appliedCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM erasure_applied`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func without(all []string, drop ...string) []string {
	skip := map[string]bool{}
	for _, d := range drop {
		skip[d] = true
	}
	var out []string
	for _, a := range all {
		if !skip[a] {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

func allSeededIDs() []string {
	var ids []string
	for _, c := range mixedCandidates() {
		ids = append(ids, c.id)
	}
	sort.Strings(ids)
	return ids
}

// fakeCore serves a DiscoveryService answering the identity capability.
type fakeCore struct {
	discoveryv1.UnimplementedDiscoveryServiceServer
	disc *erasuretest.Discovery
}

func (f fakeCore) FindByCapability(ctx context.Context, in *discoveryv1.FindByCapabilityRequest) (*discoveryv1.FindByCapabilityResponse, error) {
	return f.disc.FindByCapability(ctx, in)
}

func serveFakeCore(t *testing.T, disc *erasuretest.Discovery) string {
	t.Helper()
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	discoveryv1.RegisterDiscoveryServiceServer(gs, fakeCore{disc: disc})
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	return lis.Addr().String()
}

func TestCriteriaReferencesUser(t *testing.T) {
	tests := []struct {
		name, raw, id string
		refs, bad     bool
	}{
		{"requested by", `{"RequestedBy":"u1"}`, "u1", true, false},
		{"percent key", `{"UserWatchedPercent":{"u1":10}}`, "u1", true, false},
		{"duration key", `{"UserWatchedDurationMinutes":{"u1":1.5}}`, "u1", true, false},
		{"other user only", `{"RequestedBy":"u2","UserWatchedPercent":{"u2":1}}`, "u1", false, false},
		{"longer id with same prefix", `{"RequestedBy":"u10","UserWatchedPercent":{"u10":1}}`, "u1", false, false},
		{"shorter id that is a prefix", `{"RequestedBy":"u","UserWatchedPercent":{"u":1}}`, "u1", false, false},
		{"username is never matched", `{"RequestedBy":"alice"}`, "u1", false, false},
		{"case differs", `{"RequestedBy":"U1"}`, "u1", false, false},
		{"id as a value of an unrelated field", `{"Title":"u1","ItemID":"u1"}`, "u1", false, false},
		{"id as a map value, not a key", `{"UserWatchedPercent":{"u2":1}, "Genres":["u1"]}`, "u1", false, false},
		{"empty criteria", `{}`, "u1", false, false},
		{"empty id never matches empty requester", `{"RequestedBy":""}`, "", false, false},
		{"empty id never matches empty key", `{"UserWatchedPercent":{"":1}}`, "", false, false},
		{"unparseable with the id as a token", `{"RequestedBy":"u1"`, "u1", true, true},
		{"unparseable with a longer token", `{"RequestedBy":"u10"`, "u1", false, true},
		{"wrong shape with the id as a token", `{"UserWatchedPercent":"u1"}`, "u1", true, true},
		{"wrong shape without the id", `{"UserWatchedPercent":"u2"}`, "u1", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, bad := criteriaReferencesUser(tt.raw, tt.id)
			if refs != tt.refs || bad != tt.bad {
				t.Errorf("criteriaReferencesUser(%s, %q) = %v, %v; want %v, %v", tt.raw, tt.id, refs, bad, tt.refs, tt.bad)
			}
		})
	}
}

// TestEvalContextUserBearingFieldsAreCovered fails if EvalContext (stored
// verbatim as criteria_json) gains a per-user map or another requester field
// that criteriaUserRefs does not look at: a new user-bearing field needs an
// explicit erasure decision.
func TestEvalContextUserBearingFieldsAreCovered(t *testing.T) {
	ref := reflect.TypeFor[criteriaUserRefs]()
	var maps []string
	for f := range reflect.TypeFor[EvalContext]().Fields() {
		if f.Type.Kind() == reflect.Map && f.Type.Key().Kind() == reflect.String {
			maps = append(maps, f.Name)
			if _, ok := ref.FieldByName(f.Name); !ok {
				t.Errorf("EvalContext.%s is a string-keyed map that criteriaUserRefs does not cover", f.Name)
			}
		}
	}
	sort.Strings(maps)
	want := []string{"UserWatchedDurationMinutes", "UserWatchedPercent"}
	if !reflect.DeepEqual(maps, want) {
		t.Errorf("string-keyed maps in EvalContext = %v, want %v: decide whether the new one names a user", maps, want)
	}
	if _, ok := ref.FieldByName("RequestedBy"); !ok {
		t.Error("criteriaUserRefs lost RequestedBy")
	}
}

func TestEraseUserDeletesOnlyCandidatesNamingTheUser(t *testing.T) {
	devEnv(t)
	ctx := context.Background()
	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), nil)
	seedCandidates(t, m, mixedCandidates())
	// Unrelated tables are untouched.
	if _, err := m.db.Exec(`INSERT INTO protections (id, scope, item_id, title, reason, created_at) VALUES ('p1','movie','m1','t','manual','2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`INSERT INTO overlay_state (scope, item_id, original_poster_path, applied_at) VALUES ('movie','m2','/p.jpg','2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	counts, err := m.eraseUser(ctx, "er-1", victimID, "home")
	if err != nil {
		t.Fatal(err)
	}
	if counts[countCandidates] != 3 || counts[countCandidatesUnparseable] != 0 {
		t.Errorf("counts = %v", counts)
	}
	raw := openRaw(t, m.dbPath)
	if got, want := candidateIDs(t, raw), without(allSeededIDs(), victimCandidateIDs...); !reflect.DeepEqual(got, want) {
		t.Errorf("remaining candidates = %v, want %v", got, want)
	}
	var user, tenant string
	if err := raw.QueryRow(`SELECT user_id, tenant_id FROM erasure_applied WHERE erasure_id = 'er-1'`).Scan(&user, &tenant); err != nil || user != victimID || tenant != "home" {
		t.Errorf("erasure record = %q %q %v", user, tenant, err)
	}
	if n, err := m.countCandidatesNamingUser(ctx, victimID); err != nil || n != 0 {
		t.Errorf("post-condition = %d, %v", n, err)
	}
	var prot, overlay int
	_ = raw.QueryRow(`SELECT COUNT(*) FROM protections`).Scan(&prot)
	_ = raw.QueryRow(`SELECT COUNT(*) FROM overlay_state`).Scan(&overlay)
	if prot != 1 || overlay != 1 {
		t.Errorf("unrelated tables changed: protections=%d overlay_state=%d", prot, overlay)
	}
}

func TestEraseUserUnparseableCriteriaFailsClosed(t *testing.T) {
	devEnv(t)
	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), nil)
	seedCandidates(t, m, []seedCandidate{
		{"cand_corrupt_victim", "m1", `{"RequestedBy":"` + victimID + `"`},
		{"cand_corrupt_other", "m2", `{"RequestedBy":"` + bystanderID + `"`},
		{"cand_corrupt_longer", "m3", `{"RequestedBy":"` + victimID + `0"`},
	})
	counts, err := m.eraseUser(context.Background(), "er-1", victimID, "")
	if err != nil {
		t.Fatal(err)
	}
	if counts[countCandidates] != 1 || counts[countCandidatesUnparseable] != 1 {
		t.Errorf("counts = %v", counts)
	}
	if got, want := candidateIDs(t, openRaw(t, m.dbPath)), []string{"cand_corrupt_longer", "cand_corrupt_other"}; !reflect.DeepEqual(got, want) {
		t.Errorf("remaining = %v, want %v", got, want)
	}
}

func TestEraseUserSecondApplyIsNoOp(t *testing.T) {
	devEnv(t)
	ctx := context.Background()
	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), nil)
	seedCandidates(t, m, mixedCandidates())
	first, err := m.eraseUser(ctx, "er-1", victimID, "home")
	if err != nil {
		t.Fatal(err)
	}
	// A candidate naming the victim that appears after the erasure is not
	// touched by replaying the same erasure id: re-apply changes nothing.
	seedCandidates(t, m, []seedCandidate{{"cand_late", "m20", `{"RequestedBy":"` + victimID + `"}`}})
	second, err := m.eraseUser(ctx, "er-1", victimID, "home")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second apply counts = %v, want the recorded %v", second, first)
	}
	raw := openRaw(t, m.dbPath)
	if n := appliedCount(t, raw); n != 1 {
		t.Errorf("erasure_applied rows = %d, want 1", n)
	}
	var late int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM candidates WHERE id = 'cand_late'`).Scan(&late); err != nil || late != 1 {
		t.Errorf("replay deleted a later row: %d %v", late, err)
	}
	if ok, err := m.erasureApplied(ctx, "er-1"); err != nil || !ok {
		t.Errorf("erasureApplied = %v, %v", ok, err)
	}
	if ok, err := m.erasureApplied(ctx, "er-2"); err != nil || ok {
		t.Errorf("erasureApplied(unknown) = %v, %v", ok, err)
	}
}

// TestEraseUserTransactionFailureDoesNotMarkApplied injects a failure after
// the deletes (an aborting trigger on the applied-record insert): nothing is
// partially erased and no applied row exists. A retry completes.
func TestEraseUserTransactionFailureDoesNotMarkApplied(t *testing.T) {
	devEnv(t)
	ctx := context.Background()
	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), nil)
	seedCandidates(t, m, mixedCandidates())
	raw := openRaw(t, m.dbPath)
	if _, err := raw.Exec(`CREATE TRIGGER inject_failure BEFORE INSERT ON erasure_applied BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.eraseUser(ctx, "er-1", victimID, "home"); err == nil {
		t.Fatal("erasure succeeded despite the injected failure")
	}
	if got := candidateIDs(t, raw); !reflect.DeepEqual(got, allSeededIDs()) {
		t.Fatalf("partial erasure after failure: %v", got)
	}
	if n := appliedCount(t, raw); n != 0 {
		t.Fatalf("erasure_applied rows after failure = %d", n)
	}
	if ok, err := m.erasureApplied(ctx, "er-1"); err != nil || ok {
		t.Fatalf("erasureApplied after failure = %v, %v", ok, err)
	}

	if _, err := raw.Exec(`DROP TRIGGER inject_failure`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.eraseUser(ctx, "er-1", victimID, "home"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got, want := candidateIDs(t, raw), without(allSeededIDs(), victimCandidateIDs...); !reflect.DeepEqual(got, want) {
		t.Fatalf("after retry = %v, want %v", got, want)
	}
	if n := appliedCount(t, raw); n != 1 {
		t.Fatalf("erasure_applied rows after retry = %d", n)
	}
}

func TestEraseUserRejectsEmptyIDs(t *testing.T) {
	devEnv(t)
	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), nil)
	seedCandidates(t, m, mixedCandidates())
	for _, c := range []struct{ erasureID, userID string }{{"", victimID}, {"er-1", ""}} {
		if _, err := m.eraseUser(context.Background(), c.erasureID, c.userID, ""); err == nil {
			t.Errorf("eraseUser(%q, %q) succeeded", c.erasureID, c.userID)
		}
	}
	if got := candidateIDs(t, openRaw(t, m.dbPath)); !reflect.DeepEqual(got, allSeededIDs()) {
		t.Errorf("candidates changed: %v", got)
	}
}

// TestErasureLifecycleThroughCoreDiscovery runs the real wiring: the module
// connects to a (fake) core, discovers the identity provider through it,
// sweeps at startup, applies the ledger, and stops its reconciler goroutine
// before the database closes.
func TestErasureLifecycleThroughCoreDiscovery(t *testing.T) {
	devEnv(t)
	dbPath := filepath.Join(t.TempDir(), "maintainer.db")

	provider := erasuretest.NewProvider()
	provider.Allowed = map[string]bool{testOwnerID: true}
	providerAddr := erasuretest.ServePlain(t, provider)
	coreAddr := serveFakeCore(t, erasuretest.NewDiscovery(erasuretest.Module(testProviderID, providerAddr)))
	t.Setenv("MUXCORE_GRPC_ADDR", coreAddr)
	t.Setenv(erasure.EnvSweepInterval, "1m")

	ctx := context.Background()
	m := NewModule(Config{DBPath: dbPath, GRPCAddr: "127.0.0.1:0", ErasureTune: fastTune})
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	seedCandidates(t, m, mixedCandidates())
	eid := provider.AddErasure(victimID, "home")
	if m.Reconciler() == nil {
		t.Fatal("reconciler not configured despite a core connection")
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	done := m.erasureDone
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = m.Stop(ctx)
		}
	})

	eventually(t, "startup sweep acknowledgement", func() bool {
		a, ok := provider.Latest(eid, testOwnerID)
		return ok && a.Outcome == authv1.ErasureOutcome_ERASURE_OUTCOME_OK
	})
	ack, _ := provider.Latest(eid, testOwnerID)
	if ack.Counts[countCandidates] != 3 {
		t.Errorf("ack counts = %v", ack.Counts)
	}
	raw := openRaw(t, dbPath)
	if got, want := candidateIDs(t, raw), without(allSeededIDs(), victimCandidateIDs...); !reflect.DeepEqual(got, want) {
		t.Errorf("remaining candidates = %v, want %v", got, want)
	}

	// Second sweep: no-op (applied and already acknowledged OK).
	res, err := m.Reconciler().SweepOnce(ctx)
	if err != nil || res.Applied != 0 || res.Skipped != 1 {
		t.Fatalf("second sweep: %+v %v", res, err)
	}

	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	stopped = true
	select {
	case <-done:
	default:
		t.Fatal("reconciler goroutine still running after Stop")
	}
	if m.Reconciler() != nil || m.erasureConn != nil {
		t.Fatal("reconciler or core connection retained after Stop")
	}
}

// TestErasureSweepFailureAcksFailedThenCompletes drives the injected failure
// through the reconciler: the ack is FAILED/apply_failed, nothing changed, and
// the next sweep completes with an OK ack.
func TestErasureSweepFailureAcksFailedThenCompletes(t *testing.T) {
	devEnv(t)
	ctx := context.Background()
	provider := erasuretest.NewProvider()
	providerAddr := erasuretest.ServePlain(t, provider)
	dialer := &erasure.ProviderDialer{Discovery: erasuretest.NewDiscovery(erasuretest.Module(testProviderID, providerAddr))}
	eid := provider.AddErasure(victimID, "home")

	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), func(c *Config) { c.ErasureDialer = dialer })
	seedCandidates(t, m, mixedCandidates())
	raw := openRaw(t, m.dbPath)
	if _, err := raw.Exec(`CREATE TRIGGER inject_failure BEFORE INSERT ON erasure_applied BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	res, err := m.Reconciler().SweepOnce(ctx)
	if err == nil || res.Failed != 1 || res.Applied != 0 {
		t.Fatalf("sweep with injected failure: %+v %v", res, err)
	}
	a, ok := provider.Latest(eid, testOwnerID)
	if !ok || a.Outcome != authv1.ErasureOutcome_ERASURE_OUTCOME_FAILED || a.Detail != erasure.DetailApplyFailed {
		t.Fatalf("ack after failure: %+v %v", a, ok)
	}
	if got := candidateIDs(t, raw); !reflect.DeepEqual(got, allSeededIDs()) || appliedCount(t, raw) != 0 {
		t.Fatalf("partial erasure: %v applied=%d", got, appliedCount(t, raw))
	}

	if _, err := raw.Exec(`DROP TRIGGER inject_failure`); err != nil {
		t.Fatal(err)
	}
	res, err = m.Reconciler().SweepOnce(ctx)
	if err != nil || res.Applied != 1 || res.Acked != 1 {
		t.Fatalf("next sweep: %+v %v", res, err)
	}
	if a, _ := provider.Latest(eid, testOwnerID); a.Outcome != authv1.ErasureOutcome_ERASURE_OUTCOME_OK {
		t.Fatalf("ack after retry: %+v", a)
	}
	if got, want := candidateIDs(t, raw), without(allSeededIDs(), victimCandidateIDs...); !reflect.DeepEqual(got, want) {
		t.Fatalf("after retry = %v, want %v", got, want)
	}
}

// TestErasureVerifyCountsCandidatesNamingTheUser: Verify is the
// post-condition the reconciler acknowledges against; it counts candidates
// that still name the user and no one else's.
func TestErasureVerifyCountsCandidatesNamingTheUser(t *testing.T) {
	devEnv(t)
	ctx := context.Background()
	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), nil)
	seedCandidates(t, m, mixedCandidates())
	owner := erasureOwner{m: m}
	if n, err := owner.Verify(ctx, erasure.Tombstone{UserID: victimID}); err != nil || n != 3 {
		t.Fatalf("verify before = %d, %v", n, err)
	}
	if _, err := owner.Apply(ctx, erasure.Tombstone{ErasureID: "er-1", UserID: victimID, TenantID: "home"}); err != nil {
		t.Fatal(err)
	}
	if n, err := owner.Verify(ctx, erasure.Tombstone{UserID: victimID}); err != nil || n != 0 {
		t.Fatalf("verify after = %d, %v", n, err)
	}
	// The bystander is named by cand_bystander only once the victim's three
	// candidates (two of which also named the bystander) are gone.
	if n, err := owner.Verify(ctx, erasure.Tombstone{UserID: bystanderID}); err != nil || n != 1 {
		t.Fatalf("verify bystander = %d, %v; want 1", n, err)
	}
}

// TestErasureLedgerFromWrongCertificateNeverActedOn: the provider's TLS
// certificate chains to the mesh CA and even carries the provider id as a SAN,
// but its CN is another module: the ledger must not be read, so nothing is
// erased. With the genuine certificate the same ledger applies over mTLS.
func TestErasureLedgerFromWrongCertificateNeverActedOn(t *testing.T) {
	clearMeshEnv(t)
	ctx := context.Background()
	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), nil)
	seedCandidates(t, m, mixedCandidates())

	pki := erasuretest.NewPKI(t)
	provider := erasuretest.NewProvider()
	provider.AddErasure(victimID, "home")
	impCert, impKey := pki.Issue(t, "mallory", testProviderID, "localhost")
	addr := erasuretest.ServeTLS(t, provider, impCert, impKey, pki.CAFile)
	cc, ck := pki.Issue(t, testOwnerID)
	dialer := &erasure.ProviderDialer{Discovery: erasuretest.NewDiscovery(erasuretest.Module(testProviderID, addr)), CertFile: cc, KeyFile: ck, CAFile: pki.CAFile}
	rec, err := erasure.New(erasure.Config{Owner: erasureOwner{m: m}, Dialer: dialer, Interval: time.Minute,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), CallTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	res, err := rec.SweepOnce(ctx)
	if err == nil || res.Seen != 0 || res.Applied != 0 {
		t.Fatalf("sweep against impersonating provider: %+v %v", res, err)
	}
	raw := openRaw(t, m.dbPath)
	if got := candidateIDs(t, raw); !reflect.DeepEqual(got, allSeededIDs()) || appliedCount(t, raw) != 0 {
		t.Fatalf("impersonated ledger changed data: %v applied=%d", got, appliedCount(t, raw))
	}
	if list, _ := provider.Calls(); list != 0 {
		t.Fatalf("ledger RPC reached the impersonator %d times", list)
	}

	genuineCert, genuineKey := pki.Issue(t, testProviderID)
	provider.Allowed = map[string]bool{testOwnerID: true}
	good := erasuretest.ServeTLS(t, provider, genuineCert, genuineKey, pki.CAFile)
	dialer.Discovery = erasuretest.NewDiscovery(erasuretest.Module(testProviderID, good))
	res, err = rec.SweepOnce(ctx)
	if err != nil || res.Applied != 1 || res.Acked != 1 {
		t.Fatalf("genuine sweep: %+v %v", res, err)
	}
	if got, want := candidateIDs(t, raw), without(allSeededIDs(), victimCandidateIDs...); !reflect.DeepEqual(got, want) {
		t.Fatalf("after genuine sweep = %v, want %v", got, want)
	}
}

// TestErasureProviderUnreachableErasesNothing: discovery or the provider
// failing is never an erasure.
func TestErasureProviderUnreachableErasesNothing(t *testing.T) {
	devEnv(t)
	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), nil)
	seedCandidates(t, m, mixedCandidates())
	disc := erasuretest.NewDiscovery()
	rec, err := erasure.New(erasure.Config{Owner: erasureOwner{m: m}, Dialer: &erasure.ProviderDialer{Discovery: disc},
		Interval: time.Minute, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), CallTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.SweepOnce(context.Background()); err == nil {
		t.Fatal("sweep without a provider succeeded")
	}
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := lis.Addr().String()
	_ = lis.Close()
	disc.Set("identity", erasuretest.Module(testProviderID, dead))
	if _, err := rec.SweepOnce(context.Background()); err == nil {
		t.Fatal("sweep against a dead provider succeeded")
	}
	raw := openRaw(t, m.dbPath)
	if got := candidateIDs(t, raw); !reflect.DeepEqual(got, allSeededIDs()) || appliedCount(t, raw) != 0 {
		t.Fatalf("data changed without a ledger: %v applied=%d", got, appliedCount(t, raw))
	}
}

func TestSetupErasureConfiguration(t *testing.T) {
	t.Run("household requires a core connection", func(t *testing.T) {
		clearMeshEnv(t)
		t.Setenv("MUXCORE_PROFILE", "household")
		m := NewModule(Config{DBPath: filepath.Join(t.TempDir(), "m.db"), GRPCAddr: "127.0.0.1:0"})
		if err := m.Init(context.Background()); err == nil {
			_ = m.Stop(context.Background())
			t.Fatal("household without a core connection started")
		}
		if err := m.Health(context.Background()); err == nil {
			t.Error("database left open after a failed Init")
		}
	})
	t.Run("staging requires a core connection", func(t *testing.T) {
		clearMeshEnv(t)
		t.Setenv("MUXCORE_PROFILE", "staging")
		m := NewModule(Config{DBPath: filepath.Join(t.TempDir(), "m.db"), GRPCAddr: "127.0.0.1:0"})
		if err := m.Init(context.Background()); err == nil {
			_ = m.Stop(context.Background())
			t.Fatal("staging without a core connection started")
		}
	})
	t.Run("dev without core warns and disables", func(t *testing.T) {
		devEnv(t)
		m := newErasureTestModule(t, filepath.Join(t.TempDir(), "m.db"), nil)
		if m.Reconciler() != nil {
			t.Fatal("reconciler started without a core connection")
		}
	})
	t.Run("invalid sweep interval is a startup error", func(t *testing.T) {
		devEnv(t)
		t.Setenv(erasure.EnvSweepInterval, "soon")
		dialer := &erasure.ProviderDialer{Discovery: erasuretest.NewDiscovery()}
		m := NewModule(Config{DBPath: filepath.Join(t.TempDir(), "m.db"), GRPCAddr: "127.0.0.1:0", ErasureDialer: dialer})
		if err := m.Init(context.Background()); err == nil {
			_ = m.Stop(context.Background())
			t.Fatal("invalid ERASURE_SWEEP_INTERVAL accepted")
		}
	})
	t.Run("interval from environment", func(t *testing.T) {
		devEnv(t)
		t.Setenv(erasure.EnvSweepInterval, "2m")
		dialer := &erasure.ProviderDialer{Discovery: erasuretest.NewDiscovery()}
		m := newErasureTestModule(t, filepath.Join(t.TempDir(), "m.db"), func(c *Config) { c.ErasureDialer = dialer })
		if m.Reconciler() == nil {
			t.Fatal("reconciler not configured")
		}
	})
}

// TestPersistCandidatesRefusesErasedUser: a scan that still sees the erased
// id upstream (another owner has not applied the tombstone yet) must not put
// it back, and still persists bystanders.
func TestPersistCandidatesRefusesErasedUser(t *testing.T) {
	devEnv(t)
	ctx := context.Background()
	m := newErasureTestModule(t, filepath.Join(t.TempDir(), "maintainer.db"), nil)
	if _, err := m.eraseUser(ctx, "er-1", victimID, "home"); err != nil {
		t.Fatal(err)
	}
	mk := func(item, requestedBy string, watched map[string]float64) candidateMatch {
		return candidateMatch{
			Action:  ActionDelete,
			RuleIDs: []string{"rule_1"},
			Ctx:     EvalContext{Scope: ScopeMovie, ItemID: item, Title: item, RequestedBy: requestedBy, UserWatchedPercent: watched},
		}
	}
	matches := map[string]candidateMatch{
		"movie:a": mk("a", victimID, nil),
		"movie:b": mk("b", bystanderID, map[string]float64{victimID: 50}),
		"movie:c": mk("c", bystanderID, map[string]float64{bystanderID: 50}),
		"movie:d": mk("d", "", nil),
	}
	if got := m.persistCandidates(ctx, matches, false); got != 2 {
		t.Errorf("persisted %d candidates, want 2", got)
	}
	got := candidateIDs(t, openRaw(t, m.dbPath))
	if len(got) != 2 {
		t.Fatalf("candidates = %v", got)
	}
	if n, err := m.countCandidatesNamingUser(ctx, victimID); err != nil || n != 0 {
		t.Errorf("erased user named by %d candidates, %v", n, err)
	}
	if n, err := m.countCandidatesNamingUser(ctx, bystanderID); err != nil || n != 1 {
		t.Errorf("bystander named by %d candidates, %v; want 1", n, err)
	}
}

// TestErasureOwnerDoesNotReadOtherModuleData is a structural guard for
// ADR-0035 §2: the erasure path touches only this module's database and
// takes its input only from the reconciler. See roadmap C-39 for the
// separate, pre-existing userdata reads in requester_protect.go.
func TestErasureOwnerDoesNotReadOtherModuleData(t *testing.T) {
	banned := map[string]bool{
		"os.ReadFile": true, "os.Open": true, "os.OpenFile": true, "os.ReadDir": true, "os.Stat": true,
		"filepath.Walk": true, "filepath.WalkDir": true, "filepath.Glob": true, "ioutil.ReadFile": true,
	}
	fset := token.NewFileSet()
	for _, file := range []string{"erasure.go", "erasure_store.go"} {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && (id.Name == "userdataDir" || id.Name == "userdataDataDir") {
				t.Errorf("%s references %s", file, id.Name)
			}
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && banned[x.Name+"."+sel.Sel.Name] {
				t.Errorf("%s calls %s.%s", file, x.Name, sel.Sel.Name)
			}
			return true
		})
	}
}
