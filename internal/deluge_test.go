package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDelugeLoginAndRemove(t *testing.T) {
	removed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "auth.login":
			_, _ = w.Write([]byte(`{"result": true, "id": 1}`))
		case "core.get_torrents_status":
			_, _ = w.Write([]byte(`{"result": {"abc123": {"name":"Test","ratio":1.2,"is_seed":true,"total_done":100,"total_size":100}}, "id": 1}`))
		case "core.remove_torrent":
			removed = true
			_, _ = w.Write([]byte(`{"result": null, "id": 1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("DELUGE_URL", srv.URL)
	t.Setenv("DELUGE_PASSWORD", "secret")

	d := (&Module{httpCli: srv.Client()}).delugeClient()
	if err := d.removeDownloads(t.Context(), []string{"abc123"}, true, 0.5); err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected deluge remove call")
	}
}
