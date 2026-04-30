// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/anggorodewanto/playtesthub/pkg/discord"
	"github.com/anggorodewanto/playtesthub/pkg/service"

	"github.com/go-openapi/loads"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anggorodewanto/playtesthub/pkg/common"
	"github.com/anggorodewanto/playtesthub/pkg/config"
	"github.com/anggorodewanto/playtesthub/pkg/migrate"
	"github.com/anggorodewanto/playtesthub/pkg/repo"

	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/repository"

	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/factory"
	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/service/iam"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"

	sdkAuth "github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/utils/auth"
	prometheusGrpc "github.com/grpc-ecosystem/go-grpc-prometheus"
	prometheusCollectors "github.com/prometheus/client_golang/prometheus/collectors"
)

const (
	metricsEndpoint     = "/metrics"
	metricsPort         = 8080
	grpcServerPort      = 6565
	grpcGatewayHTTPPort = 8000
)

var serviceName = "extend-app-service-extension"

func parseSlogLevel(levelStr string) slog.Level {
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "fatal", "panic":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func newLogger(level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseSlogLevel(level)}))
}

// agsConfigRepo is the AGS SDK's ConfigRepository view of our pkg/config
// values. The SDK's default implementation reads AB_* env vars at
// construction time; our PRD uses AGS_*, so we wire the SDK through this
// thin adapter instead.
type agsConfigRepo struct {
	clientID     string
	clientSecret string
	baseURL      string
}

func (c agsConfigRepo) GetClientId() string       { return c.clientID }
func (c agsConfigRepo) GetClientSecret() string   { return c.clientSecret }
func (c agsConfigRepo) GetJusticeBaseUrl() string { return c.baseURL }

// PRD §6 Observability: NDA text, survey free-text answers, and
// Code values MUST NOT appear in logs. PayloadReceived / PayloadSent
// would dump request/response bodies verbatim — including nda_text
// on CreatePlaytest / EditPlaytest and Code.value once M2 lands —
// so we log only the call boundaries.
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

// buildOAuthService bridges pkg/config to the AGS SDK; the SDK's default
// would otherwise read AB_* env vars that PRD §5.9 does not define.
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

func installAuthInterceptors(
	ctx context.Context,
	cfg *config.Config,
	oauthService iam.OAuth20Service,
	logger *slog.Logger,
	unary []grpc.UnaryServerInterceptor,
	stream []grpc.StreamServerInterceptor,
) ([]grpc.UnaryServerInterceptor, []grpc.StreamServerInterceptor) {
	common.Validator = common.NewTokenValidator(oauthService, time.Duration(cfg.RefreshIntervalSeconds)*time.Second, true)
	if err := common.Validator.Initialize(ctx); err != nil {
		logger.Info(err.Error())
	}
	unary = append(unary, common.NewUnaryAuthServerIntercept())
	stream = append(stream, common.NewStreamAuthServerIntercept())
	logger.Info("added auth interceptors")

	return unary, stream
}

func buildPlaytesthubServer(cfg *config.Config, dbPool *pgxpool.Pool) *service.PlaytesthubServiceServer {
	playtestStore := repo.NewPgPlaytestStore(dbPool)
	applicantStore := repo.NewPgApplicantStore(dbPool)
	svcServer := service.NewPlaytesthubServiceServer(playtestStore, applicantStore, cfg.AGSNamespace)
	if botClient := discord.NewBotClient(cfg.DiscordBotToken); botClient != nil {
		svcServer = svcServer.WithDiscordLookup(botClient)
	}
	// ExchangeDiscordCode posts the Discord OAuth code to AGS IAM's
	// platform-token grant. Confidential auth (Basic) is required;
	// the public PKCE client cannot drive this endpoint.
	return svcServer.WithDiscordExchangeProxy(service.DiscordExchangeProxy{
		AGSBaseURL:   cfg.AGSBaseURL,
		ClientID:     cfg.AGSIAMClientID,
		ClientSecret: cfg.AGSIAMClientSecret,
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
	})
}

func serveGateway(grpcGateway http.Handler, logger *slog.Logger, basePath string) {
	swaggerDir := "gateway/apidocs"
	srv := newGRPCGatewayHTTPServer(fmt.Sprintf(":%d", grpcGatewayHTTPPort), grpcGateway, logger, swaggerDir, basePath)
	logger.Info("starting gRPC-Gateway HTTP server", "port", grpcGatewayHTTPPort)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("failed to run gRPC-Gateway HTTP server", "error", err)
		os.Exit(1)
	}
}

func serveMetrics(logger *slog.Logger, registry *prometheus.Registry) {
	http.Handle(metricsEndpoint, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", metricsPort), nil); err != nil {
		logger.Error("failed to start metrics server", "error", err)
		os.Exit(1)
	}
}

func serveGRPC(s *grpc.Server, logger *slog.Logger) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcServerPort))
	if err != nil {
		logger.Error("failed to listen to tcp", "port", grpcServerPort, "error", err)
		os.Exit(1)
	}
	if err := s.Serve(lis); err != nil {
		logger.Error("failed to run gRPC server", "error", err)
		os.Exit(1)
	}
}

func newPrometheusRegistry() *prometheus.Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(
		prometheusCollectors.NewGoCollector(),
		prometheusCollectors.NewProcessCollector(prometheusCollectors.ProcessCollectorOpts{}),
		prometheusGrpc.DefaultServerMetrics,
	)

	return r
}

func main() {
	bootLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		bootLogger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Apply DB migrations before anything else boots. A schema-out-of-date
	// process is never safe to serve traffic.
	if err := migrate.Up(cfg.DatabaseURL, "migrations"); err != nil {
		logger.Error("failed to apply migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	loggingOptions := buildLoggingOptions()
	unaryInterceptors := []grpc.UnaryServerInterceptor{
		prometheusGrpc.UnaryServerInterceptor,
		logging.UnaryServerInterceptor(common.InterceptorLogger(logger), loggingOptions...),
	}
	streamInterceptors := []grpc.StreamServerInterceptor{
		prometheusGrpc.StreamServerInterceptor,
		logging.StreamServerInterceptor(common.InterceptorLogger(logger), loggingOptions...),
	}

	oauthService, configRepo := buildOAuthService(cfg)
	if cfg.AuthEnabled {
		unaryInterceptors, streamInterceptors = installAuthInterceptors(ctx, cfg, oauthService, logger, unaryInterceptors, streamInterceptors)
	}

	s := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(unaryInterceptors...),
		grpc.ChainStreamInterceptor(streamInterceptors...),
	)

	// Local dev and integration smoke tests run with auth off and no AGS
	// reachability, so an unconditional LoginClient call would wedge boot.
	if cfg.AuthEnabled {
		clientId := configRepo.GetClientId()
		clientSecret := configRepo.GetClientSecret()
		if err := oauthService.LoginClient(&clientId, &clientSecret); err != nil {
			logger.Error("error unable to login using clientId and clientSecret", "error", err)
			os.Exit(1)
		}
	}

	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open database pool", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	pb.RegisterPlaytesthubServiceServer(s, buildPlaytesthubServer(cfg, dbPool))
	reflection.Register(s)
	grpc_health_v1.RegisterHealthServer(s, health.NewServer())

	grpcGateway, err := common.NewGateway(ctx, fmt.Sprintf("localhost:%d", grpcServerPort), cfg.BasePath)
	if err != nil {
		logger.Error("failed to create gRPC-Gateway", "error", err)
		os.Exit(1)
	}
	go serveGateway(grpcGateway, logger, cfg.BasePath)

	prometheusGrpc.Register(s)
	go serveMetrics(logger, newPrometheusRegistry())
	logger.Info("serving prometheus metrics", "port", metricsPort, "endpoint", metricsEndpoint)

	if cfg.OtelServiceName != "" {
		serviceName = "extend-app-se-" + strings.ToLower(cfg.OtelServiceName)
	}
	tracerProvider, err := common.NewTracerProvider(serviceName)
	if err != nil {
		logger.Error("failed to create tracer provider", "error", err)
		os.Exit(1)
	}
	otel.SetTracerProvider(tracerProvider)
	defer func(ctx context.Context) {
		if err := tracerProvider.Shutdown(ctx); err != nil {
			logger.Error("failed to shutdown tracer provider", "error", err)
			os.Exit(1)
		}
	}(ctx)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			b3.New(),
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	go serveGRPC(s, logger)
	logger.Info("app server started", "service", serviceName)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("signal received")
}

func newGRPCGatewayHTTPServer(
	addr string, handler http.Handler, logger *slog.Logger, swaggerDir, basePath string,
) *http.Server {
	// Create a new ServeMux
	mux := http.NewServeMux()

	// Add the gRPC-Gateway handler
	mux.Handle("/", handler)

	// Serve Swagger UI and JSON
	serveSwaggerUI(mux, basePath)
	serveSwaggerJSON(mux, swaggerDir, basePath)

	// Add logging middleware
	loggedMux := loggingMiddleware(logger, mux)

	return &http.Server{
		Addr:     addr,
		Handler:  loggedMux,
		ErrorLog: log.New(os.Stderr, "httpSrv: ", log.LstdFlags), // Configure the logger for the HTTP server
	}
}

// loggingMiddleware is a middleware that logs HTTP requests
func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		logger.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", duration,
		)
	})
}

func serveSwaggerUI(mux *http.ServeMux, basePath string) {
	swaggerUIDir := "third_party/swagger-ui"
	fileServer := http.FileServer(http.Dir(swaggerUIDir))
	swaggerUiPath := fmt.Sprintf("%s/apidocs/", basePath)
	mux.Handle(swaggerUiPath, http.StripPrefix(swaggerUiPath, fileServer))
}

func serveSwaggerJSON(mux *http.ServeMux, swaggerDir, basePath string) {
	fileHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		matchingFiles, err := filepath.Glob(filepath.Join(swaggerDir, "*.swagger.json"))
		if err != nil || len(matchingFiles) == 0 {
			http.Error(w, "Error finding Swagger JSON file", http.StatusInternalServerError)

			return
		}

		firstMatchingFile := matchingFiles[0]
		swagger, err := loads.Spec(firstMatchingFile)
		if err != nil {
			http.Error(w, "Error parsing Swagger JSON file", http.StatusInternalServerError)

			return
		}

		// Update the base path
		swagger.Spec().BasePath = basePath

		updatedSwagger, err := swagger.Spec().MarshalJSON()
		if err != nil {
			http.Error(w, "Error serializing updated Swagger JSON", http.StatusInternalServerError)

			return
		}
		var prettySwagger bytes.Buffer
		err = json.Indent(&prettySwagger, updatedSwagger, "", "  ")
		if err != nil {
			http.Error(w, "Error formatting updated Swagger JSON", http.StatusInternalServerError)

			return
		}

		_, err = w.Write(prettySwagger.Bytes())
		if err != nil {
			http.Error(w, "Error writing Swagger JSON response", http.StatusInternalServerError)

			return
		}
	})
	apidocsPath := fmt.Sprintf("%s/apidocs/api.json", basePath)
	mux.Handle(apidocsPath, fileHandler)
}
