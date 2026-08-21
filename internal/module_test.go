package internal

import (
	"context"
	"path/filepath"
	"testing"

	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
)

func TestModuleInitHealth(t *testing.T) {
	dir := t.TempDir()
	m := NewModule(Config{
		DBPath:   filepath.Join(dir, "maintainer.db"),
		GRPCAddr: "127.0.0.1:0",
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertRuleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewModule(Config{
		DBPath:   filepath.Join(dir, "maintainer.db"),
		GRPCAddr: "127.0.0.1:0",
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(ctx)

	resp, err := m.UpsertRule(ctx, &maintainv1.UpsertRuleRequest{Rule: &maintainv1.RuleGroup{
		Name:           "Old unwatched movies",
		Enabled:        true,
		Scope:          maintainv1.MediaScope_MEDIA_SCOPE_MOVIE,
		DefinitionJson: `{"op":"and","conditions":[{"field":"watch.never_watched","operator":"equals","value":true}]}`,
		Outcome:        maintainv1.RuleOutcome_RULE_OUTCOME_CANDIDATE,
		ArrAction:      maintainv1.ArrAction_ARR_ACTION_DELETE,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetRule().GetId() == "" {
		t.Fatal("expected rule id")
	}
	list, err := m.ListRules(ctx, &maintainv1.ListRulesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.GetRules()) != 1 {
		t.Fatalf("got %d rules", len(list.GetRules()))
	}
}

func TestScanNowDryRunEmptyLibrary(t *testing.T) {
	dir := t.TempDir()
	m := NewModule(Config{
		DBPath:   filepath.Join(dir, "maintainer.db"),
		GRPCAddr: "127.0.0.1:0",
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(ctx)

	resp, err := m.ScanNow(ctx, &maintainv1.ScanNowRequest{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetCandidatesFound() != 0 {
		t.Fatalf("expected 0 candidates, got %d", resp.GetCandidatesFound())
	}
}
