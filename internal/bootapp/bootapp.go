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

	"github.com/anggorodewanto/playtesthub/pkg/common"
	"github.com/anggorodewanto/playtesthub/pkg/config"
	"github.com/anggorodewanto/playtesthub/pkg/discord"
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

	pb.RegisterPlaytesthubServiceServer(grpcSrv, buildPlaytesthubServer(opts.Config, opts.DBPool, httpClient))
	reflection.Register(grpcSrv)
	grpc_health_v1.RegisterHealthServer(grpcSrv, health.NewServer())
	prometheusGrpc.Register(grpcSrv)

	return &Server{
		GRPC:     grpcSrv,
		listener: opts.Listener,
		logger:   logger,
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

func buildPlaytesthubServer(cfg *config.Config, dbPool *pgxpool.Pool, httpClient *http.Client) *service.PlaytesthubServiceServer {
	playtestStore := repo.NewPgPlaytestStore(dbPool)
	applicantStore := repo.NewPgApplicantStore(dbPool)
	ndaStore := repo.NewPgNDAAcceptanceStore(dbPool)
	auditStore := repo.NewPgAuditLogStore(dbPool)
	codeStore := repo.NewPgCodeStore(dbPool)
	txRunner := repo.NewPgTxRunner(dbPool)
	svcServer := service.NewPlaytesthubServiceServer(playtestStore, applicantStore, cfg.AGSNamespace).
		WithNDAStore(ndaStore).
		WithAuditLogStore(auditStore).
		WithCodeStore(codeStore).
		WithTxRunner(txRunner)
	if botClient := discord.NewBotClient(cfg.DiscordBotToken); botClient != nil {
		svcServer = svcServer.WithDiscordLookup(botClient)
	}
	return svcServer.WithDiscordExchangeProxy(service.DiscordExchangeProxy{
		AGSBaseURL:   cfg.AGSBaseURL,
		ClientID:     cfg.AGSIAMClientID,
		ClientSecret: cfg.AGSIAMClientSecret,
		HTTPClient:   httpClient,
	})
}
