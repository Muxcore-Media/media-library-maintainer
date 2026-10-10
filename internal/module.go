package internal

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"

	ffprobev1 "github.com/Muxcore-Media/media-ffprobe/proto/ffprobev1"
	manifest "github.com/Muxcore-Media/media-library-maintainer"
	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
	monitorv1 "github.com/Muxcore-Media/playback-monitor/proto/monitorv1"
	requestmedia "github.com/Muxcore-Media/request-media/proto/requestmedia"

	"github.com/Muxcore-Media/core/pkg/contracts"
	"github.com/Muxcore-Media/core/sdk/go/client"
	modulesdk "github.com/Muxcore-Media/core/sdk/go/module"
	"github.com/Muxcore-Media/core/sdk/go/module/erasure"
	_ "modernc.org/sqlite"
)

type Module struct {
	maintainv1.UnimplementedMaintainerServiceServer
	erasureDialer                *erasure.ProviderDialer
	erasureTune                  func(*erasure.Config)
	reconciler                   *erasure.Reconciler
	erasureConn                  *grpc.ClientConn
	erasureCancel                context.CancelFunc
	erasureDone                  chan struct{}
	moviesClient                 mgmntv1.MovieManagementServiceClient
	ffprobeClient                ffprobev1.AnalysisServiceClient
	grpcLis                      net.Listener
	playbackClient               monitorv1.PlaybackMonitorServiceClient
	requestClient                requestmedia.RequestServiceClient
	tvClient                     tvmgmtv1.TvManagementServiceClient
	grpcSrv                      *grpc.Server
	db                           *sql.DB
	mc                           *client.Client
	tvConn                       *grpc.ClientConn
	requestUserMap               map[string]string
	requestsConn                 *grpc.ClientConn
	ffprobeConn                  *grpc.ClientConn
	playbackConn                 *grpc.ClientConn
	moviesConn                   *grpc.ClientConn
	httpCli                      *http.Client
	schedulerCancel              context.CancelFunc
	overlayTemplateJSON          string
	userdataDataDir              string
	overlayTitleCardTemplateJSON string
	id                           string
	overlayPillTextColor         string
	overlayPillColor             string
	overlayBarColor              string
	downloadClientURL            string
	overlayDateFormat            string
	movePath                     string
	dbPath                       string
	freeUpRootPath               string
	grpcAddr                     string
	downloadClientPass           string
	downloadClientUser           string
	diskActMaxFreePercent        float64
	maxActionsPerRun             int
	scanInterval                 time.Duration
	erasureInterval              time.Duration
	downloadClientFallbackRatio  float64
	actInterval                  time.Duration
	protectRequestMinAgeDays     int
	protectRequestMaxDays        int
	mu                           sync.RWMutex
	cfgMu                        sync.RWMutex
	addListExclusionOnDelete     bool
	overlayShowDate              bool
	notifyEnabled                bool
	overlayEnabled               bool
	dryRun                       bool
	autoActEnabled               bool
	overlayTitleCardEnabled      bool
	protectUnwatchedRequesters   bool
	downloadClientDeleteDataSet  bool
	downloadClientDeleteData     bool
	leavingSoonNotifyEnabled     bool
}

type Config struct {
	// ErasureDialer overrides discovery of the identity provider through the
	// core connection (tests). ErasureInterval overrides
	// ERASURE_SWEEP_INTERVAL; ErasureTune adjusts the reconciler config.
	ErasureDialer   *erasure.ProviderDialer
	ErasureTune     func(*erasure.Config)
	ID              string
	DBPath          string
	GRPCAddr        string
	ErasureInterval time.Duration
}

func NewModule(cfg Config) *Module {
	if cfg.ID == "" {
		cfg.ID = "media-library-maintainer"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "/var/lib/media-library-maintainer/maintainer.db"
	}
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = ":9545"
	}
	if v := os.Getenv("MAINTAINER_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("MAINTAINER_GRPC_ADDR"); v != "" {
		cfg.GRPCAddr = v
	}
	return &Module{
		id:             cfg.ID,
		dbPath:         cfg.DBPath,
		grpcAddr:       cfg.GRPCAddr,
		httpCli:        &http.Client{Timeout: 30 * time.Second},
		requestUserMap: make(map[string]string),

		erasureDialer:   cfg.ErasureDialer,
		erasureInterval: cfg.ErasureInterval,
		erasureTune:     cfg.ErasureTune,
	}
}

func (m *Module) Info() contracts.ModuleInfo {
	return contracts.ModuleInfo{
		ID:          m.id,
		Name:        "Media Library Maintainer",
		Version:     modulesdk.ManifestVersion(manifest.ManifestJSON),
		Roles:       []string{"library-maintainer", "cleanup"},
		Description: "Automated library maintenance with rules, grace periods, and cleanup actions",
		Author:      "MuxCore",
		Capabilities: []string{
			"media.library.maintainer",
			"media.cleanup",
			"settings",
		},
		MinCoreVersion: "0.4.0",
		HTTPAddr:       m.grpcAddr,
	}
}

func (m *Module) Init(ctx context.Context) error {
	if err := m.initDB(ctx); err != nil {
		return err
	}
	if err := m.setupErasure(); err != nil {
		m.closeDB()
		return err
	}
	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", m.grpcAddr)
	if err != nil {
		m.closeErasureConn()
		m.closeDB()
		return fmt.Errorf("listen gRPC: %w", err)
	}
	m.grpcLis = lis
	slog.Info("media-library-maintainer initialized", "db", m.dbPath, "grpc", m.grpcAddr)
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.grpcSrv = grpc.NewServer()
	maintainv1.RegisterMaintainerServiceServer(m.grpcSrv, m)
	modulesdk.RegisterSettings(m.grpcSrv, m.id, m)
	go func() {
		slog.Info("media-library-maintainer gRPC started", "addr", m.grpcAddr)
		if err := m.grpcSrv.Serve(m.grpcLis); err != nil {
			slog.Error("media-library-maintainer gRPC error", "error", err)
		}
	}()
	schedCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.schedulerCancel = cancel
	go m.schedulerLoop(schedCtx)
	go m.dialCore(context.WithoutCancel(ctx)) //nolint:gosec // background mesh dial for module lifetime
	m.startErasure(ctx)
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	// The reconciler writes to the database: stop and wait for it first.
	m.stopErasure(ctx)
	if m.schedulerCancel != nil {
		m.schedulerCancel()
	}
	if m.grpcSrv != nil {
		m.grpcSrv.GracefulStop()
	}
	if m.moviesConn != nil {
		_ = m.moviesConn.Close()
	}
	if m.tvConn != nil {
		_ = m.tvConn.Close()
	}
	if m.requestsConn != nil {
		_ = m.requestsConn.Close()
	}
	if m.playbackConn != nil {
		_ = m.playbackConn.Close()
	}
	if m.ffprobeConn != nil {
		_ = m.ffprobeConn.Close()
	}
	if m.mc != nil {
		_ = m.mc.Close()
	}
	m.closeDB()
	slog.Info("media-library-maintainer stopped")
	return nil
}

func (m *Module) closeDB() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db != nil {
		_ = m.db.Close()
		m.db = nil
	}
}

func (m *Module) Health(ctx context.Context) error {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("not initialized")
	}
	return db.PingContext(ctx)
}

var _ contracts.Module = (*Module)(nil)
