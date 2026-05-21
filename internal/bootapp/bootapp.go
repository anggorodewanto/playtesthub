// Package bootapp wires the playtesthub gRPC server in-process so both
// main.go and the e2e suite can construct identical servers without
// duplicating the interceptor + service-registration plumbing.
//
// Scope is deliberately narrow: gRPC server, interceptors (logging +
// optional AGS IAM auth + Prometheus), the playtesthub service handler,
// reflection, health. Out of scope (kept in main.go because e2e doesn't
// need them): grpc-gateway HTTP server, metrics endpoint, OTEL tracer,
// swagger UI, signal handling.
package bootapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/factory"
	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/repository"
	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/service/iam"
	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/service/platform"
	sdkAuth "github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/utils/auth"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	prometheusGrpc "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/anggorodewanto/playtesthub/internal/reclaim"
	"github.com/anggorodewanto/playtesthub/internal/window"
	"github.com/anggorodewanto/playtesthub/pkg/adt"
	"github.com/anggorodewanto/playtesthub/pkg/ags"
	"github.com/anggorodewanto/playtesthub/pkg/common"
	"github.com/anggorodewanto/playtesthub/pkg/config"
	"github.com/anggorodewanto/playtesthub/pkg/discord"
	"github.com/anggorodewanto/playtesthub/pkg/dmqueue"
	iampkg "github.com/anggorodewanto/playtesthub/pkg/iam"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
	"github.com/anggorodewanto/playtesthub/pkg/service"
)

// Options configures a Server. Config + DBPool + Listener are required;
// every other field is optional with sensible defaults.
type Options struct {
	// Config is the parsed runtime configuration. Auth interceptors are
	// installed when Config.AuthEnabled is true.
	Config *config.Config

	// DBPool is caller-owned. bootapp does not Close it; tests share one
	// pool across multiple bootapp.New calls.
	DBPool *pgxpool.Pool

	// Listener is where the gRPC server will Serve. Tests pass a
	// listener bound to ":0" for a free port; production passes
	// net.Listen("tcp", ":6565").
	Listener net.Listener

	// Logger is used for boot-time messages and the gRPC logging
	// interceptor. Defaults to a JSON slog at info level on stderr.
	Logger *slog.Logger

	// HTTPClient is used by the DiscordExchangeProxy. Defaults to a
	// 10-second-timeout client.
	HTTPClient *http.Client
}

// Server holds the constructed gRPC server and its bound listener.
// Lifecycle: New → Serve (blocking, in a goroutine) → Stop.
type Server struct {
	GRPC     *grpc.Server
	listener net.Listener
	logger   *slog.Logger

	// DMQueue is the bounded in-memory DM queue (PRD §5.4 + dm-queue.md).
	// Exposed so main.go can run the worker + restart sweep with a
	// shared instance; the e2e suite's bootapp constructions ignore it.
	DMQueue *dmqueue.Queue

	// PlatformLogin runs a client-credentials grant against AGS and
	// seeds the TokenRepository the SDK-backed AGS adapter consumes.
	// nil when no SDK adapter is wired (AGS_STORE_ID unset or auth
	// disabled). main.go calls it during boot; the e2e suite ignores
	// it because tests run AuthEnabled=false.
	PlatformLogin func() error
}

// New constructs a Server from opts. It does not Serve; the caller
// invokes Serve() (typically in a goroutine) once it is ready for the
// listener to accept traffic.
func New(ctx context.Context, opts Options) (*Server, error) {
	if opts.Config == nil {
		return nil, errors.New("bootapp: Options.Config is required")
	}
	if opts.DBPool == nil {
		return nil, errors.New("bootapp: Options.DBPool is required")
	}
	if opts.Listener == nil {
		return nil, errors.New("bootapp: Options.Listener is required")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	loggingOptions := buildLoggingOptions()
	unaryInterceptors := []grpc.UnaryServerInterceptor{
		prometheusGrpc.UnaryServerInterceptor,
		logging.UnaryServerInterceptor(common.InterceptorLogger(logger), loggingOptions...),
	}
	streamInterceptors := []grpc.StreamServerInterceptor{
		prometheusGrpc.StreamServerInterceptor,
		logging.StreamServerInterceptor(common.InterceptorLogger(logger), loggingOptions...),
	}

	if opts.Config.AuthEnabled {
		oauthService, _ := buildOAuthService(opts.Config)
		// Initialize is best-effort: a transient AGS hiccup at boot
		// must not wedge the process. Validator's per-request path
		// will refresh on demand.
		common.Validator = common.NewTokenValidator(oauthService, time.Duration(opts.Config.RefreshIntervalSeconds)*time.Second, true)
		if err := common.Validator.Initialize(ctx); err != nil {
			logger.Info("token validator initialize failed (will retry per-request)", "error", err.Error())
		}
		unaryInterceptors = append(unaryInterceptors, common.NewUnaryAuthServerIntercept())
		streamInterceptors = append(streamInterceptors, common.NewStreamAuthServerIntercept())
	}

	grpcSrv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(unaryInterceptors...),
		grpc.ChainStreamInterceptor(streamInterceptors...),
	)

	svcServer, dmQueue, platformLogin := buildPlaytesthubServer(opts.Config, opts.DBPool, httpClient, logger)
	pb.RegisterPlaytesthubServiceServer(grpcSrv, svcServer)
	reflection.Register(grpcSrv)
	grpc_health_v1.RegisterHealthServer(grpcSrv, health.NewServer())
	prometheusGrpc.Register(grpcSrv)

	return &Server{
		GRPC:          grpcSrv,
		listener:      opts.Listener,
		logger:        logger,
		DMQueue:       dmQueue,
		PlatformLogin: platformLogin,
	}, nil
}

// Addr returns the listener's address as "host:port" so callers
// (especially tests) can construct dial targets without poking at the
// listener directly.
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Serve blocks on grpc.Server.Serve(listener). Returns nil when Stop is
// called (gRPC's documented contract: Serve returns nil after
// GracefulStop), and the underlying error otherwise.
func (s *Server) Serve() error {
	if err := s.GRPC.Serve(s.listener); err != nil {
		return fmt.Errorf("grpc serve: %w", err)
	}
	return nil
}

// Stop drains in-flight RPCs and closes the listener.
func (s *Server) Stop() {
	s.GRPC.GracefulStop()
}

// PRD §6 Observability: NDA text, survey free-text answers, and Code
// values MUST NOT appear in logs. PayloadReceived / PayloadSent would
// dump request/response bodies verbatim — including nda_text on
// CreatePlaytest / EditPlaytest and Code.value once M2 lands — so we
// log only the call boundaries.
func buildLoggingOptions() []logging.Option {
	return []logging.Option{
		logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
		logging.WithFieldsFromContext(func(ctx context.Context) logging.Fields {
			if span := trace.SpanContextFromContext(ctx); span.IsSampled() {
				return logging.Fields{"traceID", span.TraceID().String()}
			}
			return nil
		}),
		logging.WithLevels(logging.DefaultClientCodeToLevel),
		logging.WithDurationField(logging.DurationToDurationField),
	}
}

// agsConfigRepo bridges pkg/config to the AGS SDK's ConfigRepository
// view; the SDK's default reads AB_* env vars that PRD §5.9 does not
// define.
type agsConfigRepo struct {
	clientID     string
	clientSecret string
	baseURL      string
}

func (c agsConfigRepo) GetClientId() string       { return c.clientID }
func (c agsConfigRepo) GetClientSecret() string   { return c.clientSecret }
func (c agsConfigRepo) GetJusticeBaseUrl() string { return c.baseURL }

// BuildOAuthService is exported so main.go can reuse the same wiring
// for its LoginClient call (kept in main.go because the e2e suite does
// not need a pre-warmed client-credentials grant).
func BuildOAuthService(cfg *config.Config) (iam.OAuth20Service, repository.ConfigRepository) {
	return buildOAuthService(cfg)
}

func buildOAuthService(cfg *config.Config) (iam.OAuth20Service, repository.ConfigRepository) {
	var tokenRepo repository.TokenRepository = sdkAuth.DefaultTokenRepositoryImpl()
	var configRepo repository.ConfigRepository = agsConfigRepo{
		clientID:     cfg.AGSIAMClientID,
		clientSecret: cfg.AGSIAMClientSecret,
		baseURL:      cfg.AGSBaseURL,
	}
	var refreshRepo repository.RefreshTokenRepository = &sdkAuth.RefreshTokenImpl{RefreshRate: 0.8, AutoRefresh: true}

	return iam.OAuth20Service{
		Client:                 factory.NewIamClient(configRepo),
		TokenRepository:        tokenRepo,
		RefreshTokenRepository: refreshRepo,
		ConfigRepository:       configRepo,
	}, configRepo
}

func buildPlaytesthubServer(cfg *config.Config, dbPool *pgxpool.Pool, httpClient *http.Client, logger *slog.Logger) (*service.PlaytesthubServiceServer, *dmqueue.Queue, func() error) {
	playtestStore := repo.NewPgPlaytestStore(dbPool)
	applicantStore := repo.NewPgApplicantStore(dbPool)
	ndaStore := repo.NewPgNDAAcceptanceStore(dbPool)
	auditStore := repo.NewPgAuditLogStore(dbPool)
	codeStore := repo.NewPgCodeStore(dbPool)
	surveyStore := repo.NewPgSurveyStore(dbPool)
	surveyResponseStore := repo.NewPgSurveyResponseStore(dbPool)
	txRunner := repo.NewPgTxRunner(dbPool)

	botClient := discord.NewBotClient(cfg.DiscordBotToken)
	var dmSender dmqueue.Sender = dmqueue.SenderFunc(noopDMSender)
	if botClient != nil {
		dmSender = botClient
		logger.Info("dm sender: real Discord bot client")
	} else {
		logger.Info("dm sender: noop (DISCORD_BOT_TOKEN unset; queue + audit pipeline still exercised)")
	}
	dmQueue := dmqueue.New(dmqueue.Config{
		MaxDepth:        cfg.DMQueueMaxDepth,
		DrainRatePerSec: cfg.DMDrainRatePerSec,
		Namespace:       cfg.AGSNamespace,
	}, dmSender, applicantStore, auditStore, logger)

	// AGS_CAMPAIGN initial-create wires through pkg/ags. The SDK-backed
	// adapter (phase 8.1) is enabled when AuthEnabled is true; AGS_STORE_ID
	// is now optional (M2 phase 16: Bootstrap auto-discovers / auto-creates
	// the store, category, and currency on first AGS_CAMPAIGN create).
	// MemClient remains the fallback for dev/e2e boots with auth disabled
	// so the full AGS_CAMPAIGN code path runs without outbound calls.
	var agsClient ags.Client
	var platformLogin func() error
	if cfg.AuthEnabled {
		platformConfigRepo := ags.NewPlatformConfigRepository(cfg.AGSBaseURL, cfg.AGSIAMClientID, cfg.AGSIAMClientSecret)
		platformClient := factory.NewPlatformClient(platformConfigRepo)
		// One TokenRepository instance is shared between the OAuth
		// service that runs LoginClient (seeded by Server.PlatformLogin)
		// and the Item / Campaign / Store / Category / Currency services
		// that consume the token on each outbound AGS call. The SDK
		// auto-refreshes via RefreshTokenImpl per the same pattern as
		// buildOAuthService.
		tokenRepo := sdkAuth.DefaultTokenRepositoryImpl()
		refreshRepo := &sdkAuth.RefreshTokenImpl{RefreshRate: 0.8, AutoRefresh: true}
		oauthSvc := iam.OAuth20Service{
			Client:                 factory.NewIamClient(platformConfigRepo),
			TokenRepository:        tokenRepo,
			RefreshTokenRepository: refreshRepo,
			ConfigRepository:       platformConfigRepo,
		}
		itemSvc := &platform.ItemService{
			Client:           platformClient,
			ConfigRepository: platformConfigRepo,
			TokenRepository:  tokenRepo,
		}
		campaignSvc := &platform.CampaignService{
			Client:           platformClient,
			ConfigRepository: platformConfigRepo,
			TokenRepository:  tokenRepo,
		}
		storeSvc := &platform.StoreService{
			Client:           platformClient,
			ConfigRepository: platformConfigRepo,
			TokenRepository:  tokenRepo,
		}
		categorySvc := &platform.CategoryService{
			Client:           platformClient,
			ConfigRepository: platformConfigRepo,
			TokenRepository:  tokenRepo,
		}
		currencySvc := &platform.CurrencyService{
			Client:           platformClient,
			ConfigRepository: platformConfigRepo,
			TokenRepository:  tokenRepo,
		}
		platformLogin = func() error {
			id := cfg.AGSIAMClientID
			secret := cfg.AGSIAMClientSecret
			return oauthSvc.LoginClient(&id, &secret)
		}
		agsClient = ags.NewSDKClient(ags.SDKClientOptions{
			Namespace:   cfg.AGSNamespace,
			StoreID:     cfg.AGSStoreID,
			ItemSvc:     ags.NewPlatformItemService(itemSvc),
			CampaignSvc: ags.NewPlatformCampaignService(campaignSvc),
			StoreSvc:    ags.NewPlatformStoreService(storeSvc),
			CategorySvc: ags.NewPlatformCategoryService(categorySvc),
			CurrencySvc: ags.NewPlatformCurrencyService(currencySvc),
			// SDK's auto-refresh goroutine is process-global (sync.Once)
			// and the inbound auth surface claims it first, leaving the
			// platform-side token un-refreshed. SDKClient compensates by
			// calling Login on HTTP 401 and retrying once.
			Login: platformLogin,
			// Region pricing — AGS rejects CreateItem without a fully
			// formed RegionData entry for the store's defaultRegion.
			// Bootstrap auto-discovers / auto-creates a VIRTUAL currency
			// when AGS_REGION_CURRENCY_CODE is unset.
			RegionCurrencyCode: cfg.AGSRegionCurrencyCode,
			RegionCurrencyType: cfg.AGSRegionCurrencyType,
			RegionCode:         cfg.AGSRegionCode,
		})
		storeIDLog := cfg.AGSStoreID
		if storeIDLog == "" {
			storeIDLog = "(auto-discover via Bootstrap)"
		}
		logger.Info("ags client: SDK-backed", "namespace", cfg.AGSNamespace, "storeId", storeIDLog)
	} else {
		agsClient = ags.NewMemClient()
		logger.Info("ags client: in-memory (enable auth to use the live SDK adapter)")
	}

	// ADT distribution model (M5.B / STATUS_M5.md Track B). The live
	// HTTP-backed adapter is enabled when AuthEnabled && ADTBaseURL is
	// set and AGS IAM creds are available — those creds mint the
	// service JWT the adapter attaches to every ADT call. Otherwise
	// (dev / smoke / e2e / boots without ADT config) we fall back to
	// MemClient so the full ADT code path is still exercised without
	// an outbound round-trip.
	var adtClient adt.Client
	if cfg.AuthEnabled && cfg.ADTBaseURL != "" && cfg.AGSBaseURL != "" && cfg.AGSIAMClientID != "" && cfg.AGSIAMClientSecret != "" {
		adtLookup := &iampkg.AGSAdminPlatformLookup{
			HTTPClient:   httpClient,
			BaseURL:      cfg.AGSBaseURL,
			Namespace:    cfg.AGSNamespace,
			ClientID:     cfg.AGSIAMClientID,
			ClientSecret: cfg.AGSIAMClientSecret,
		}
		adtClient = adt.NewHTTPClient(cfg.ADTBaseURL, httpClient, adtLookup.AdminToken)
		logger.Info("adt client: HTTP-backed", "baseUrl", cfg.ADTBaseURL)
	} else {
		adtClient = adt.NewMemClient()
		logger.Info("adt client: in-memory (set ADT_BASE_URL + enable auth to use the live HTTP adapter)")
	}

	adtLinkageStore := repo.NewPgADTLinkageStore(dbPool)
	announcementStore := repo.NewPgAnnouncementStore(dbPool)
	var announcementSender service.AnnouncementSender
	if botClient != nil {
		announcementSender = botClient
	}

	leaderStore := repo.NewPgLeaderStore(dbPool)
	workers := []service.WorkerInfo{{
		Name:         reclaim.LeaseName,
		TickInterval: time.Duration(cfg.ReclaimIntervalSeconds) * time.Second,
		LeaseTTL:     time.Duration(cfg.LeaderLeaseTTLSeconds) * time.Second,
	}}
	if cfg.WindowTickSeconds > 0 {
		workers = append(workers, service.WorkerInfo{
			Name:         window.LeaseName,
			TickInterval: time.Duration(cfg.WindowTickSeconds) * time.Second,
			LeaseTTL:     time.Duration(cfg.LeaderLeaseTTLSeconds) * time.Second,
		})
	}

	svcServer := service.NewPlaytesthubServiceServer(playtestStore, applicantStore, cfg.AGSNamespace).
		WithNDAStore(ndaStore).
		WithAuditLogStore(auditStore).
		WithCodeStore(codeStore).
		WithSurveyStore(surveyStore).
		WithSurveyResponseStore(surveyResponseStore).
		WithTxRunner(txRunner).
		WithDMQueue(dmQueue).
		WithPlayerBaseURL(cfg.PlayerBaseURL).
		WithAGSClient(agsClient).
		WithAGSCodeBatchSize(cfg.AGSCodeBatchSize).
		WithADTLinkageStore(adtLinkageStore).
		WithADTClient(adtClient).
		WithADTLinkConfig(service.ADTLinkConfig{
			ADTBaseURL:        cfg.ADTBaseURL,
			RedirectBaseURL:   cfg.ADTRedirectBaseURL,
			PendingTTLSeconds: cfg.ADTLinkagePendingTTLSeconds,
		}).
		WithStudioNamespaceResolver(buildStudioNamespaceResolver(cfg, httpClient)).
		WithAnnouncementStore(announcementStore, announcementSender).
		WithAnnouncementBounds(cfg.AnnouncementSubjectMaxLen, cfg.AnnouncementMessageMaxLen).
		WithWorkerHealth(leaderStore, workers).
		WithLogger(logger)
	if botClient != nil {
		svcServer = svcServer.WithDiscordLookup(botClient)
	}
	if cfg.AGSBaseURL != "" && cfg.AGSIAMClientID != "" && cfg.AGSIAMClientSecret != "" {
		svcServer = svcServer.WithPlatformLookup(&iampkg.AGSAdminPlatformLookup{
			HTTPClient:   httpClient,
			BaseURL:      cfg.AGSBaseURL,
			Namespace:    cfg.AGSNamespace,
			ClientID:     cfg.AGSIAMClientID,
			ClientSecret: cfg.AGSIAMClientSecret,
		})
	}
	return svcServer.WithDiscordExchangeProxy(service.DiscordExchangeProxy{
		AGSBaseURL:   cfg.AGSBaseURL,
		ClientID:     cfg.AGSIAMClientID,
		ClientSecret: cfg.AGSIAMClientSecret,
		HTTPClient:   httpClient,
	}), dmQueue, platformLogin
}

// buildStudioNamespaceResolver returns the ADT-linkage studio resolver.
// In production it mints a client-credentials AGS service IAM token
// (via AGSAdminPlatformLookup) and reads `union_namespace ?? namespace`
// off the resulting JWT — this is what ADT will see on every
// downstream API call from playtesthub, so the linkage row MUST be
// keyed on the same value (PRD §4.8.2).
//
// In dev / smoke / e2e (auth disabled or AGS creds absent), no service
// token is mintable; the resolver falls back to cfg.AGSNamespace as a
// stand-in for the studio identity. This is deliberately permissive
// for tests + smoke probes — the live boot path always has the AGS
// creds + a real token.
func buildStudioNamespaceResolver(cfg *config.Config, httpClient *http.Client) service.StudioNamespaceResolver {
	if cfg.AGSBaseURL == "" || cfg.AGSIAMClientID == "" || cfg.AGSIAMClientSecret == "" {
		// Dev / smoke / e2e fallback: linkage rows are scoped to the
		// configured AGS_NAMESPACE so single-namespace tests work
		// end-to-end without minting a real JWT.
		ns := cfg.AGSNamespace
		return func(_ context.Context) (string, error) { return ns, nil }
	}
	lookup := &iampkg.AGSAdminPlatformLookup{
		HTTPClient:   httpClient,
		BaseURL:      cfg.AGSBaseURL,
		Namespace:    cfg.AGSNamespace,
		ClientID:     cfg.AGSIAMClientID,
		ClientSecret: cfg.AGSIAMClientSecret,
	}
	return func(ctx context.Context) (string, error) {
		return lookup.GetStudioNamespace(ctx)
	}
}

// noopDMSender is the fallback Sender used when DISCORD_BOT_TOKEN is
// empty (dev / e2e / smoke). It returns nil (success) so the queue +
// worker + audit/marking pipeline is exercised end-to-end without
// making outbound Discord calls. Production with a configured bot
// token uses *discord.BotClient (M3 phase 7).
func noopDMSender(_ context.Context, _, _ string) error { return nil }
