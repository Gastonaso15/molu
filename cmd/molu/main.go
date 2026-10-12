package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Gastonaso15/molu/pkg/config"
	"github.com/Gastonaso15/molu/pkg/exec"
	"github.com/Gastonaso15/molu/pkg/obs"
	"github.com/Gastonaso15/molu/pkg/schema"
	"github.com/ha1tch/xolu/pkg/client"
)

func main() {

	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuración inválida:\n%v\n", err)
		os.Exit(1)
	}
	obs.InitLogger(cfg.LogLevel, cfg.LogFormat)

	slog.Info("Starting Molu Frontend", "xolu_url", cfg.XoluURL, "transport", cfg.Transport)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	// Cliente de xolu. Por ahora solo se usa para la sonda (/ready no
	// requiere credencial); la autenticación se agrega en otro paso.
	xolu := client.New(cfg.XoluURL)

	probe := exec.NewProbe(xolu, exec.ProbeConfig{
		Interval:           cfg.PingInterval,
		Timeout:            cfg.PingTimeout,
		Freshness:          cfg.PongFreshness,
		FailFloor:          cfg.PingFailFloor,
		FailCeiling:        cfg.PingFailCeiling,
		StartupMaxAttempts: cfg.StartupMaxAttempts,
	})

	// Schema Loader (Spec 003): carga inicial con retry/backoff
	schemaClient := schema.NewXoluSchemaClient(cfg.XoluURL, cfg.SchemaTimeout)
	schemaLoader := schema.NewSchemaLoader(schemaClient, schema.LoaderConfig{
		RefreshInterval: cfg.SchemaRefreshInterval,
		RetryFloor:      cfg.SchemaRetryFloor,
		RetryCeiling:    cfg.SchemaRetryCeiling,
		MaxAttempts:     cfg.SchemaMaxAttempts,
		Timeout:         cfg.SchemaTimeout,
	})

	// §8.4: no seguimos hasta que xolu responda el primer pong.
	if err := probe.WaitReady(ctx); err != nil {
		if ctx.Err() == nil {
			slog.Error("xolu is not available, exiting", "error", err)
			os.Exit(1)
		}
	} else {
		go probe.Run(ctx)
	}

	// Carga inicial de schemas (con retry/backoff)
	if err := schemaLoader.Load(ctx); err != nil {
		if ctx.Err() == nil {
			slog.Error("failed to load schemas from xolu, exiting", "error", err)
			os.Exit(1)
		}
	}

	// Registrar primitivos genéricos en MCP server (sin namespace)
	primitives := schemaLoader.GetPrimitives()
	for _, p := range primitives {
		slog.Info("Registering primitive", "name", p.Name)
		// TODO: register p in MCP server
	}

	// Hot-reload: ticker
	schemaLoader.Run(ctx)

	go func() {
		for sig := range sigCh {
			switch sig {
			case os.Interrupt, syscall.SIGTERM:
				slog.Info("Received signal, shutting down", "signal", sig.String())
				cancel()
			case syscall.SIGHUP:
				slog.Info("Received SIGHUP, refreshing schemas")
				if err := schemaLoader.Refresh(ctx); err != nil {
					slog.Error("schema refresh failed", "error", err)
				} else {
					slog.Info("Schemas refreshed successfully")
					// TODO: update MCP server tool registry with diff
				}
			}
		}
	}()

	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "Molu Frontend shutting down")
}