package internal

import (
	"path/filepath"
	"testing"
)

func TestMoveDestStaysUnderRoot(t *testing.T) {
	root := t.TempDir()
	got, err := moveDest(root, "/var/lib/library/Movie.mkv")
	if err != nil {
		t.Fatal(err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(realRoot, "Movie.mkv")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, err := moveDest(root, ".."); err == nil {
		t.Fatal("expected parent segment to be refused")
	}
}
