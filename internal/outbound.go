package internal

import (
	"net/http"
	"time"

	"github.com/Muxcore-Media/core/sdk/go/module/netguard"
)

// maintainerGuardOptions is the Integration profile for operator-configured
// peers (Arr, Plex, Jellyfin, download clients) and public metadata APIs.
// Private LAN and loopback stay allowed. Link-local, cloud metadata, and
// non-HTTP schemes stay refused.
func maintainerGuardOptions(timeout time.Duration) netguard.Options {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return netguard.Options{
		AllowPrivate:  true,
		AllowLoopback: true,
		Timeout:       timeout,
	}
}

func newGuardedClient(timeout time.Duration) *http.Client {
	return netguard.NewClient(netguard.Integration, maintainerGuardOptions(timeout))
}
