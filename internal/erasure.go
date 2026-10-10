package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"google.golang.org/grpc"

	discoveryv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/discovery/v1"
	modulesdk "github.com/Muxcore-Media/core/sdk/go/module"
	"github.com/Muxcore-Media/core/sdk/go/module/erasure"
	"github.com/Muxcore-Media/core/sdk/go/module/meshtls"
)

// erasureOwner is the maintainer's ADR-0035 personal-data owner: it applies
// ledger tombstones to the maintainer's own candidate rows and nothing else.
// Its only input is the Reconciler, which reads solely the verified identity
// provider's ledger. It never reads another module's files or data.
type erasureOwner struct {
	m *Module
}

var (
	_ erasure.Owner    = erasureOwner{}
	_ erasure.Verifier = erasureOwner{}
)

// ModuleID implements erasure.Owner.
func (o erasureOwner) ModuleID() string { return o.m.id }

// Applied implements erasure.Owner.
func (o erasureOwner) Applied(ctx context.Context, erasureID string) (bool, error) {
	return o.m.erasureApplied(ctx, erasureID)
}

// Apply implements erasure.Owner: one transaction deletes the candidates that
// name the user and records the erasure.
func (o erasureOwner) Apply(ctx context.Context, t erasure.Tombstone) (erasure.Counts, error) {
	counts, err := o.m.eraseUser(ctx, t.ErasureID, t.UserID, t.TenantID)
	if err != nil {
		return nil, err
	}
	return erasure.Counts(counts), nil
}

// Verify implements erasure.Verifier: candidates still naming the user.
func (o erasureOwner) Verify(ctx context.Context, t erasure.Tombstone) (int, error) {
	return o.m.countCandidatesNamingUser(ctx, t.UserID)
}

// ErasureSweepInterval is the environment variable read for the sweep period.
const ErasureSweepInterval = erasure.EnvSweepInterval

// erasureRequired reports whether the profile makes the reconciler mandatory:
// household (and its alias staging) must not run without it.
func erasureRequired() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MUXCORE_PROFILE"))) {
	case "household", "staging":
		return true
	}
	return false
}

// setupErasure builds the reconciler. It runs from Init, which modulesdk.Run
// calls after mesh enrollment, so MUXCORE_TLS_CERT/KEY/CA point at the
// module's own identity. With a core connection it discovers the exclusive
// identity provider through core; without one the reconciler is not started
// (dev/tests), which household and staging refuse.
func (m *Module) setupErasure() error {
	dialer := m.erasureDialer
	if dialer == nil {
		if strings.TrimSpace(os.Getenv("MUXCORE_GRPC_ADDR")) == "" {
			if erasureRequired() {
				return errors.New("erasure reconciler: household profile requires a core connection (MUXCORE_GRPC_ADDR)")
			}
			slog.Warn("media-library-maintainer erasure reconciler disabled: no core connection (MUXCORE_GRPC_ADDR unset); user erasures from the identity ledger are not applied")
			return nil
		}
		conn, err := modulesdk.Connect(modulesdk.ConnectConfig{Insecure: meshtls.Insecure()})
		if err != nil {
			return fmt.Errorf("erasure reconciler: connect to core: %w", err)
		}
		m.erasureConn = conn
		dialer = &erasure.ProviderDialer{Discovery: discoveryClient{conn: conn}}
	}
	cfg := erasure.Config{
		Owner:    erasureOwner{m: m},
		Dialer:   dialer,
		Logger:   slog.Default(),
		Interval: m.erasureInterval,
	}
	if m.erasureTune != nil {
		m.erasureTune(&cfg)
	}
	rec, err := erasure.New(cfg)
	if err != nil {
		m.closeErasureConn()
		return err
	}
	m.reconciler = rec
	return nil
}

func (m *Module) closeErasureConn() {
	if m.erasureConn != nil {
		_ = m.erasureConn.Close()
		m.erasureConn = nil
	}
}

// discoveryClient adapts a core connection to erasure.CapabilityFinder.
type discoveryClient struct{ conn *grpc.ClientConn }

func (d discoveryClient) FindByCapability(ctx context.Context, in *discoveryv1.FindByCapabilityRequest, opts ...grpc.CallOption) (*discoveryv1.FindByCapabilityResponse, error) {
	return discoveryv1.NewDiscoveryServiceClient(d.conn).FindByCapability(ctx, in, opts...)
}

// startErasure runs the reconciler until stopErasure. Like the scheduler it
// outlives the Start call's context and is ended only by stopErasure.
func (m *Module) startErasure(ctx context.Context) {
	if m.reconciler == nil || m.erasureCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	m.erasureCancel, m.erasureDone = cancel, done
	rec := m.reconciler
	go func() {
		defer close(done)
		if err := rec.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("media-library-maintainer erasure reconciler stopped", "error", err)
		}
	}()
}

// stopErasure cancels the reconciler and waits for it, so no sweep touches the
// database after Stop closes it.
func (m *Module) stopErasure(ctx context.Context) {
	if m.erasureCancel != nil {
		m.erasureCancel()
		select {
		case <-m.erasureDone:
		case <-ctx.Done():
			// Bounded by the caller's shutdown deadline; sweeps honour ctx
			// cancellation, so this only trips on a hung local database.
			slog.Warn("media-library-maintainer erasure reconciler did not stop before the shutdown deadline")
		}
		m.erasureCancel, m.erasureDone = nil, nil
	}
	m.reconciler = nil
	m.closeErasureConn()
}

// Reconciler exposes the erasure reconciler (nil when disabled) for tests and
// operator triggers.
func (m *Module) Reconciler() *erasure.Reconciler { return m.reconciler }
