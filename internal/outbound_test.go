package internal

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/Muxcore-Media/core/sdk/go/module/netguard"
)

func TestGuardedClientBlocksMetadata(t *testing.T) {
	m := NewModule(Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	for _, raw := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/",
		"file:///etc/passwd",
	} {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.httpCli.Do(req)
		if !errors.Is(err, netguard.ErrBlocked) {
			t.Fatalf("%s: err=%v", raw, err)
		}
	}
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.httpCli.Do(req)
	if errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("loopback refused: %v", err)
	}
}
