package internal

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Muxcore-Media/core/sdk/go/module/erasure"
	"github.com/Muxcore-Media/core/sdk/go/module/erasure/erasuretest"
	"github.com/Muxcore-Media/core/sdk/go/module/moduletest"
)

// TestUpgradeFromV0115Snapshot opens a database written by v0.1.15 (before
// ADR-0035) with the current code (ADR-0015, NFR-DATA-002, FR-INS-005): the
// forward migration creates an empty erasure_applied, existing candidates
// survive until a sweep, and the first sweep erases only the named user's.
//
// The fixture was produced by running v0.1.15's initDB and seeding rows:
// cand_1 names the victim as requester, cand_2 in a watch map, cand_3 only
// the bystander, cand_4 nobody.
func TestUpgradeFromV0115Snapshot(t *testing.T) {
	devEnv(t)
	ctx := context.Background()
	path := moduletest.CopyFixture(t, filepath.Join("testdata", "upgrade", "v0.1.15.db"))

	// The snapshot really is the previous schema.
	before := openRaw(t, path)
	var tables int
	if err := before.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'erasure_applied'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("fixture already has erasure_applied (%d, %v): it is not a pre-ADR-0035 snapshot", tables, err)
	}
	wantCandidates := []string{"cand_1", "cand_2", "cand_3", "cand_4"}
	if got := candidateIDs(t, before); !reflect.DeepEqual(got, wantCandidates) {
		t.Fatalf("fixture candidates = %v", got)
	}
	_ = before.Close()

	// Open twice: the startup migration must be idempotent.
	first := NewModule(Config{DBPath: path, GRPCAddr: "127.0.0.1:0", ErasureTune: fastTune})
	if err := first.Init(ctx); err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := first.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	provider := erasuretest.NewProvider()
	providerAddr := erasuretest.ServePlain(t, provider)
	dialer := &erasure.ProviderDialer{Discovery: erasuretest.NewDiscovery(erasuretest.Module(testProviderID, providerAddr))}
	m := NewModule(Config{DBPath: path, GRPCAddr: "127.0.0.1:0", ErasureDialer: dialer, ErasureTune: fastTune})
	if err := m.Init(ctx); err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })

	// erasure_applied exists and is empty; nothing was deleted by opening.
	if n := appliedCount(t, m.db); n != 0 {
		t.Errorf("erasure_applied rows after upgrade = %d, want 0", n)
	}
	if ok, err := m.erasureApplied(ctx, "er-upgrade"); err != nil || ok {
		t.Errorf("erasureApplied after upgrade: %v %v", ok, err)
	}
	if got := candidateIDs(t, m.db); !reflect.DeepEqual(got, wantCandidates) {
		t.Errorf("candidates after upgrade = %v, want all of %v", got, wantCandidates)
	}

	// The schema is a superset of a fresh install's.
	fresh := newErasureTestModule(t, filepath.Join(t.TempDir(), "fresh.db"), nil)
	moduletest.RequireSchemaSuperset(t, moduletest.Schema(t, m.db), moduletest.Schema(t, fresh.db))

	// Existing rows still read back through the module's own code.
	var title, criteria string
	if err := m.db.QueryRow(`SELECT title, criteria_json FROM candidates WHERE id = 'cand_2'`).Scan(&title, &criteria); err != nil || title != "Watched by victim" {
		t.Errorf("cand_2 = %q %q %v", title, criteria, err)
	}
	var prot int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM protections`).Scan(&prot); err != nil || prot != 1 {
		t.Errorf("protections after upgrade = %d, %v", prot, err)
	}
	if got := m.scanInterval.Minutes(); got != 90 {
		t.Errorf("persisted scan interval after upgrade = %v min, want 90", got)
	}

	// First sweep on the upgraded database.
	eid := provider.AddErasure(victimID, "home")
	res, err := m.Reconciler().SweepOnce(ctx)
	if err != nil || res.Applied != 1 || res.Acked != 1 {
		t.Fatalf("sweep after upgrade: %+v %v", res, err)
	}
	if got, want := candidateIDs(t, m.db), []string{"cand_3", "cand_4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("candidates after sweep = %v, want %v", got, want)
	}
	if ack, ok := provider.Latest(eid, testOwnerID); !ok || ack.Counts[countCandidates] != 2 {
		t.Errorf("ack = %+v %v", ack, ok)
	}
	if n, err := m.countCandidatesNamingUser(ctx, victimID); err != nil || n != 0 {
		t.Errorf("post-condition after upgrade = %d, %v", n, err)
	}
	moduletest.RequireIntegrity(t, m.db)
}
