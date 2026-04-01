package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flwrenn/bastion/indexer/internal/api"
	"github.com/flwrenn/bastion/indexer/internal/db"
	"github.com/flwrenn/bastion/indexer/internal/indexer"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}

	indexerConfig, err := indexer.LoadConfigFromEnv()
	if err != nil {
		return fmt.Errorf("load indexer config: %w", err)
	}

	// Connect to PostgreSQL and run pending migrations.
	// Bounded so the process fails fast if the DB is unreachable.
	startupCtx, startupCancel := context.WithTimeout(ctx, 15*time.Second)
	defer startupCancel()

	pool, err := db.Connect(startupCtx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := db.Migrate(startupCtx, pool); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	svc, err := indexer.New(indexerConfig, pool)
	if err != nil {
		return fmt.Errorf("init indexer service: %w", err)
	}

	workerResultCh := make(chan error, 1)
	go func() {
		runErr := svc.Run(ctx)
		if runErr != nil {
			cancel()
		}
		workerResultCh <- runErr
	}()

	mux := http.NewServeMux()

	// API routes — CORS enabled for frontend access.
	apiMux := http.NewServeMux()
	apiHandler := api.New(pool)
	apiHandler.Register(apiMux)
	mux.Handle("/api/", api.CORS(apiMux))

	// Health endpoints — no CORS (internal probes only).
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"bastion-indexer","status":"ok"}`)
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"name":"bastion-indexer","status":"unhealthy","error":%q}`, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"bastion-indexer","status":"ok"}`)
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Shut down gracefully on signal, draining in-flight requests.
	go func() {
		<-ctx.Done()
		slog.Info("shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	slog.Info("indexer API listening", "port", port)
	listenErr := srv.ListenAndServe()
	if listenErr != nil && listenErr != http.ErrServerClosed {
		cancel()
	}

	workerErr := <-workerResultCh

	if listenErr != nil && listenErr != http.ErrServerClosed {
		if workerErr != nil {
			return fmt.Errorf("server: %w (indexer worker: %v)", listenErr, workerErr)
		}
		return fmt.Errorf("server: %w", listenErr)
	}

	if workerErr != nil {
		return fmt.Errorf("indexer worker: %w", workerErr)
	}

	return nil
}
