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

	"github.com/go-openapi/loads"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	prometheusGrpc "github.com/grpc-ecosystem/go-grpc-prometheus"
	prometheusCollectors "github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/anggorodewanto/playtesthub/internal/bootapp"
	"github.com/anggorodewanto/playtesthub/internal/reclaim"
	"github.com/anggorodewanto/playtesthub/pkg/common"
	"github.com/anggorodewanto/playtesthub/pkg/config"
	"github.com/anggorodewanto/playtesthub/pkg/migrate"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
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

	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open database pool", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcServerPort))
	if err != nil {
		logger.Error("failed to listen to tcp", "port", grpcServerPort, "error", err)
		os.Exit(1)
	}

	server, err := bootapp.New(ctx, bootapp.Options{
		Config:   cfg,
		DBPool:   dbPool,
		Listener: listener,
		Logger:   logger,
	})
	if err != nil {
		logger.Error("failed to construct app server", "error", err)
		os.Exit(1)
	}

	// Local dev and integration smoke tests run with auth off and no AGS
	// reachability, so an unconditional LoginClient call would wedge boot.
	if cfg.AuthEnabled {
		oauthService, configRepo := bootapp.BuildOAuthService(cfg)
		clientId := configRepo.GetClientId()
		clientSecret := configRepo.GetClientSecret()
		if err := oauthService.LoginClient(&clientId, &clientSecret); err != nil {
			logger.Error("error unable to login using clientId and clientSecret", "error", err)
			os.Exit(1)
		}
	}

	grpcGateway, err := common.NewGateway(ctx, fmt.Sprintf("localhost:%d", grpcServerPort), cfg.BasePath)
	if err != nil {
		logger.Error("failed to create gRPC-Gateway", "error", err)
		os.Exit(1)
	}
	go serveGateway(grpcGateway, logger, cfg.BasePath)

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

	go func() {
		if err := server.Serve(); err != nil {
			logger.Error("failed to run gRPC server", "error", err)
			os.Exit(1)
		}
	}()
	logger.Info("app server started", "service", serviceName, "addr", server.Addr())

	reclaimWorker := reclaim.New(reclaim.Config{
		HolderID:          holderIDFromEnv(),
		LeaseTTL:          time.Duration(cfg.LeaderLeaseTTLSeconds) * time.Second,
		HeartbeatInterval: time.Duration(cfg.LeaderHeartbeatSeconds) * time.Second,
		ReclaimInterval:   time.Duration(cfg.ReclaimIntervalSeconds) * time.Second,
		ReservationTTL:    time.Duration(cfg.ReservationTTLSeconds) * time.Second,
	}, repo.NewPgLeaderStore(dbPool), repo.NewPgCodeStore(dbPool), logger)
	go func() {
		if err := reclaimWorker.Run(ctx); err != nil {
			logger.Error("reclaim worker stopped with error", "error", err)
		}
	}()
	logger.Info("reclaim worker started",
		"leaseHolder", holderIDFromEnv(),
		"reclaimIntervalSeconds", cfg.ReclaimIntervalSeconds,
		"leaseTtlSeconds", cfg.LeaderLeaseTTLSeconds)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("signal received")
	server.Stop()
}

// holderIDFromEnv returns a stable per-replica identifier for the
// leader_lease.holder column. Production callers populate POD_NAME
// (or HOSTNAME, depending on platform); a hostname fallback keeps the
// elected leader visible in logs even when neither is set.
func holderIDFromEnv() string {
	for _, key := range []string{"POD_NAME", "HOSTNAME"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "playtesthub"
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
