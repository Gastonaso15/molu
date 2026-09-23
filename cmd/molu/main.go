package molu

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Gastonaso15/molu/pkg/config"
	"github.com/Gastonaso15/molu/pkg/obs"
)

func main() {

	cfg := config.LoadFromEnv()
	obs.InitLogger(cfg.LogLevel, "text")

	slog.Info("Starting Molu Frontend", "tenant", cfg.Tenant, "transport", cfg.Transport)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("Received signal, shutting down", "signal", sig.String())
		cancel()
	}()

	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "Molu Frontend shutting down")
}
