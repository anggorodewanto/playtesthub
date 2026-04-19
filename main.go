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

func main() {
	bootLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		bootLogger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	slogLevel := parseSlogLevel(cfg.LogLevel)
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	basePath := cfg.BasePath

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Apply DB migrations before anything else boots. A schema-out-of-date
	// process is never safe to serve traffic.
	if err := migrate.Up(cfg.DatabaseURL, "migrations"); err != nil {
		logger.Error("failed to apply migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	loggingOptions := []logging.Option{
		logging.WithLogOnEvents(logging.StartCall, logging.FinishCall, logging.PayloadReceived, logging.PayloadSent),
		logging.WithFieldsFromContext(func(ctx context.Context) logging.Fields {
			if span := trace.SpanContextFromContext(ctx); span.IsSampled() {
				return logging.Fields{"traceID", span.TraceID().String()}
			}

			return nil
		}),
		logging.WithLevels(logging.DefaultClientCodeToLevel),
		logging.WithDurationField(logging.DurationToDurationField),
	}

	unaryServerInterceptors := []grpc.UnaryServerInterceptor{
		prometheusGrpc.UnaryServerInterceptor,
		logging.UnaryServerInterceptor(common.InterceptorLogger(logger), loggingOptions...),
	}
	streamServerInterceptors := []grpc.StreamServerInterceptor{
		prometheusGrpc.StreamServerInterceptor,
		logging.StreamServerInterceptor(common.InterceptorLogger(logger), loggingOptions...),
	}

	// Preparing the IAM authorization. configRepo bridges pkg/config to the
	// AGS SDK; the SDK's default would otherwise read AB_* env vars that
	// PRD §5.9 does not define.
	var tokenRepo repository.TokenRepository = sdkAuth.DefaultTokenRepositoryImpl()
	var configRepo repository.ConfigRepository = agsConfigRepo{
		clientID:     cfg.AGSIAMClientID,
		clientSecret: cfg.AGSIAMClientSecret,
		baseURL:      cfg.AGSBaseURL,
	}
	var refreshRepo repository.RefreshTokenRepository = &sdkAuth.RefreshTokenImpl{RefreshRate: 0.8, AutoRefresh: true}

	oauthService := iam.OAuth20Service{
		Client:                 factory.NewIamClient(configRepo),
		TokenRepository:        tokenRepo,
		RefreshTokenRepository: refreshRepo,
		ConfigRepository:       configRepo,
	}

	if cfg.AuthEnabled {
		common.Validator = common.NewTokenValidator(oauthService, time.Duration(cfg.RefreshIntervalSeconds)*time.Second, true)
		if err := common.Validator.Initialize(ctx); err != nil {
			logger.Info(err.Error())
		}

		unaryServerInterceptor := common.NewUnaryAuthServerIntercept()
		serverServerInterceptor := common.NewStreamAuthServerIntercept()

		unaryServerInterceptors = append(unaryServerInterceptors, unaryServerInterceptor)
		streamServerInterceptors = append(streamServerInterceptors, serverServerInterceptor)
		logger.Info("added auth interceptors")
	}

	// Create gRPC Server
	s := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(unaryServerInterceptors...),
		grpc.ChainStreamInterceptor(streamServerInterceptors...),
	)

	// Configure IAM authorization. Only attempted when auth is enabled —
	// local dev and integration smoke tests run with auth off and no AGS
	// reachability, so an unconditional LoginClient call would wedge boot.
	if cfg.AuthEnabled {
		clientId := configRepo.GetClientId()
		clientSecret := configRepo.GetClientSecret()
		if err := oauthService.LoginClient(&clientId, &clientSecret); err != nil {
			logger.Error("error unable to login using clientId and clientSecret", "error", err)
			os.Exit(1)
		}
	}

	// Open the Postgres pool used by every handler and register the
	// playtesthub.v1 service. Signup / GetApplicantStatus still route
	// to UnimplementedPlaytesthubServiceServer until M1 phase 7.
	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open database pool", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	playtestStore := repo.NewPgPlaytestStore(dbPool)
	applicantStore := repo.NewPgApplicantStore(dbPool)
	svcServer := service.NewPlaytesthubServiceServer(playtestStore, applicantStore, cfg.AGSNamespace)
	if botClient := discord.NewBotClient(cfg.DiscordBotToken); botClient != nil {
		svcServer = svcServer.WithDiscordLookup(botClient)
	}
	pb.RegisterPlaytesthubServiceServer(s, svcServer)

	// Enable gRPC Reflection
	reflection.Register(s)

	// Enable gRPC Health Check
	grpc_health_v1.RegisterHealthServer(s, health.NewServer())

	// Create a new HTTP server for the gRPC-Gateway
	grpcGateway, err := common.NewGateway(ctx, fmt.Sprintf("localhost:%d", grpcServerPort), basePath)
	if err != nil {
		logger.Error("failed to create gRPC-Gateway", "error", err)
		os.Exit(1)
	}

	// Start the gRPC-Gateway HTTP server
	go func() {
		swaggerDir := "gateway/apidocs" // Path to swagger directory
		grpcGatewayHTTPServer := newGRPCGatewayHTTPServer(fmt.Sprintf(":%d", grpcGatewayHTTPPort), grpcGateway, logger, swaggerDir, basePath)
		logger.Info("starting gRPC-Gateway HTTP server", "port", grpcGatewayHTTPPort)
		if err := grpcGatewayHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("failed to run gRPC-Gateway HTTP server", "error", err)
			os.Exit(1)
		}
	}()

	prometheusGrpc.Register(s)

	// Register Prometheus Metrics
	prometheusRegistry := prometheus.NewRegistry()
	prometheusRegistry.MustRegister(
		prometheusCollectors.NewGoCollector(),
		prometheusCollectors.NewProcessCollector(prometheusCollectors.ProcessCollectorOpts{}),
		prometheusGrpc.DefaultServerMetrics,
	)

	go func() {
		http.Handle(metricsEndpoint, promhttp.HandlerFor(prometheusRegistry, promhttp.HandlerOpts{}))
		if err := http.ListenAndServe(fmt.Sprintf(":%d", metricsPort), nil); err != nil {
			logger.Error("failed to start metrics server", "error", err)
			os.Exit(1)
		}
	}()
	logger.Info("serving prometheus metrics", "port", metricsPort, "endpoint", metricsEndpoint)

	// Set Tracer Provider
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

	// Set Text Map Propagator
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			b3.New(),
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	// Start gRPC Server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcServerPort))
	if err != nil {
		logger.Error("failed to listen to tcp", "port", grpcServerPort, "error", err)
		os.Exit(1)
	}
	go func() {
		if err = s.Serve(lis); err != nil {
			logger.Error("failed to run gRPC server", "error", err)
			os.Exit(1)
		}
	}()

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
