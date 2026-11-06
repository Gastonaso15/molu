package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Gastonaso15/molu/pkg/hub"
)

func run(ctx context.Context, getenv func(string) string) error {
	addr := getenv("MOLU_HUB_HTTP_ADDR")
	if addr == "" {
		addr = ":8091"
	}

	providerName := getenv("MOLU_HUB_RPI_PROVIDER")
	if providerName == "" {
		providerName = "admit-all"
	}
	rulesFile := getenv("MOLU_HUB_RPI_RULES_FILE")
	rulesJSON := getenv("MOLU_HUB_RPI_RULES_JSON")

	registry := hub.NewRegistry()
	provider, err := hub.NewRPIProvider(providerName, rulesFile, rulesJSON)
	if err != nil {
		return fmt.Errorf("failed to initialize RPI provider: %w", err)
	}

	discovery := hub.NewDiscovery(registry, provider)
	hubServer := hub.NewHubHTTPServer(registry, discovery)

	srv := &http.Server{
		Addr:         addr,
		Handler:      hubServer.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server error: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http server shutdown error: %w", err)
		}
		return nil
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "hub error: %v\n", err)
		os.Exit(1)
	}
}

