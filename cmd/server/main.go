// Command server runs the web page analyzer HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/isuruRuhu/web-page-analyzer/internal/analyzer"
	"github.com/isuruRuhu/web-page-analyzer/internal/web"
)

const (
	// fetchTimeout limits how long fetching a page to analyze may take.
	fetchTimeout = 15 * time.Second
	// shutdownTimeout limits how long the server will wait for graceful shutdown.
	shutdownTimeout = 60 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run starts the servers and blocks until one fails or SIGINT/SIGTERM arrives.
func run() error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		return fmt.Errorf("parse LOG_LEVEL: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	errorLog := slog.NewLogLogger(logger.Handler(), slog.LevelError)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	allowPrivate, err := strconv.ParseBool(getenv("ALLOW_PRIVATE_ADDRESSES", "false"))
	if err != nil {
		return fmt.Errorf("parse ALLOW_PRIVATE_ADDRESSES: %w", err)
	}
	client := analyzer.NewClient(fetchTimeout, allowPrivate)
	srv := &http.Server{
		Addr:         getenv("ADDR", ":9090"),
		Handler:      web.NewHandler(logger, analyzer.New(client), reg),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
		ErrorLog:     errorLog,
	}
	// Metrics and pprof listen only on localhost unless DEBUG_ADDR says
	// otherwise. There is no write timeout, since a CPU profile or trace
	// runs for as many seconds as the caller asks for.
	debugSrv := &http.Server{
		Addr:        getenv("DEBUG_ADDR", "localhost:6060"),
		Handler:     web.NewDebugHandler(reg),
		ReadTimeout: 10 * time.Second,
		IdleTimeout: 120 * time.Second,
		ErrorLog:    errorLog,
	}

	errCh := make(chan error, 2)
	serve(logger, "app", srv, errCh)
	serve(logger, "debug", debugSrv, errCh)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	// Restore default signal handling so a second Ctrl-C exits immediately.
	stop()

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	// The debug server stops last so metrics can be scraped while requests drain.
	appErr := srv.Shutdown(shutdownCtx)
	debugErr := debugSrv.Shutdown(shutdownCtx)
	if err := errors.Join(appErr, debugErr); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("server stopped")
	return nil
}

// serve runs srv in the background and sends the error it stops with to errCh.
func serve(logger *slog.Logger, name string, srv *http.Server, errCh chan<- error) {
	go func() {
		logger.Info("starting server", "name", name, "addr", srv.Addr)
		errCh <- fmt.Errorf("serve %s: %w", name, srv.ListenAndServe())
	}()
}

// getenv returns the value of key, or fallback if it is unset or empty.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
