package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Gastonaso15/molu/pkg/config"
	"github.com/Gastonaso15/molu/pkg/exec"
	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/mcp"
	"github.com/Gastonaso15/molu/pkg/obs"
	"github.com/Gastonaso15/molu/pkg/schema"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/telemetry"
	"github.com/Gastonaso15/molu/pkg/xolu"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuración inválida:\n%v\n", err)
		os.Exit(1)
	}

	// Initialize legacy logger for backward compatibility
	obs.InitLogger(cfg.LogLevel, cfg.LogFormat)

	// Initialize telemetry (Spec 007)
	telemetryCfg := telemetry.LoadConfigFromEnv()
	telemetryCfg.Component = telemetry.ComponentMoluFront

	logger := telemetry.NewLogger(telemetryCfg)
	metrics := telemetry.NewMetrics()
	health := telemetry.NewHealth()

	// Start telemetry HTTP server if enabled
	var telemetryServer *http.Server
	if telemetryCfg.TelemetryEnabled {
		telemetryMux := http.NewServeMux()
		telemetryMux.Handle(telemetryCfg.TelemetryMetricsPath, metrics.Handler())
		telemetryMux.Handle(telemetryCfg.TelemetryHealthPath, health.Handler())
		telemetryServer = &http.Server{
			Addr:         fmt.Sprintf(":%d", telemetryCfg.TelemetryPort),
			Handler:      telemetryMux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		}
		go func() {
			if err := telemetryServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.System().Error().Err(err).Msg("telemetry server failed")
			}
		}()
		logger.System().Info().
			Int("port", telemetryCfg.TelemetryPort).
			Str("metrics_path", telemetryCfg.TelemetryMetricsPath).
			Str("health_path", telemetryCfg.TelemetryHealthPath).
			Msg("telemetry server started")
	}

	logger.System().Info().
		Str("version", "dev").
		Str("xolu_url", cfg.XoluURL).
		Str("transport", cfg.Transport).
		Msg("Starting Molu Frontend")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	// Cliente de xolu
	xoluClient := xolu.NewHTTPXoluClient(cfg.XoluURL, cfg.PingTimeout)
	xoluClient.SetLogger(logger)
	xoluClient.SetMetrics(metrics)

	probe := exec.NewProbe(xoluClient, exec.ProbeConfig{
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
			logger.System().Error().Err(err).Msg("xolu is not available, exiting")
			os.Exit(1)
		}
	} else {
		go probe.Run(ctx)
	}

	// Carga inicial de schemas (con retry/backoff)
	if err := schemaLoader.Load(ctx); err != nil {
		if ctx.Err() == nil {
			logger.System().Error().Err(err).Msg("failed to load schemas from xolu, exiting")
			os.Exit(1)
		}
	}

	// Descubrimiento del Hub filtrado por RPI (Spec 005): solo si hay proveedor
	// configurado; sin él, tools/list sigue sirviendo los primitivos de xolu.
	var discovery mcp.ToolDiscoverer
	if cfg.HubRPIProvider != "" {
		rpiProvider, err := hub.NewRPIProvider(cfg.HubRPIProvider, cfg.HubRPIRulesFile, cfg.HubRPIRulesJSON)
		if err != nil {
			logger.System().Error().Err(err).Msg("invalid RPI provider configuration, exiting")
			os.Exit(1)
		}
		rpiProvider.SetLogger(logger)
		rpiProvider.SetMetrics(metrics)
		discovery = hub.NewDiscovery(hub.NewRegistry(), rpiProvider)
		logger.System().Info().Str("rpi_provider", cfg.HubRPIProvider).Msg("Hub discovery enabled")
	}

	// Inicializar Servidor MCP (Spec 006)
	mcpServer := mcp.NewServer(mcp.ServerConfig{
		Provider:    &loaderProvider{loader: schemaLoader},
		Health:      probe,
		Discovery:   discovery,
		CallTimeout: cfg.CallTimeout,
	})

	primitives := schemaLoader.GetPrimitives()
	for _, p := range primitives {
		logger.System().Info().Str("name", p.Name).Msg("Registering primitive")
	}

	// Iniciar servidor MCP sobre stdio
	go func() {
		if err := mcpServer.ServeStdio(ctx); err != nil && ctx.Err() == nil {
			logger.System().Error().Err(err).Msg("MCP stdio server terminated")
			cancel()
		}
	}()

	// Hot-reload: ticker
	schemaLoader.Run(ctx)

	// Update health with xolu status periodically
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := xoluClient.Ping(ctx); err != nil {
					health.SetXoluStatus(false, time.Now())
				} else {
					health.SetXoluStatus(true, time.Now())
				}
			}
		}
	}()

	go func() {
		for sig := range sigCh {
			switch sig {
			case os.Interrupt, syscall.SIGTERM:
				logger.System().Info().Str("signal", sig.String()).Msg("Received signal, shutting down")
				cancel()
			case syscall.SIGHUP:
				logger.System().Info().Msg("Received SIGHUP, refreshing schemas")
				if err := schemaLoader.Refresh(ctx); err != nil {
					logger.System().Error().Err(err).Msg("schema refresh failed")
				} else {
					logger.System().Info().Msg("Schemas refreshed successfully")
					mcpServer.UpdatePrimitives(schemaLoader.GetPrimitives())
				}
			}
		}
	}()

	<-ctx.Done()

	// Shutdown telemetry server
	if telemetryCfg.TelemetryEnabled {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := telemetryServer.Shutdown(shutdownCtx); err != nil {
			logger.System().Error().Err(err).Msg("telemetry server shutdown error")
		}
	}

	logger.System().Info().
		Str("uptime", time.Since(startTime).String()).
		Msg("Molu Frontend shutting down")
}

var startTime = time.Now()

type loaderProvider struct {
	loader *schema.SchemaLoader
}

func (lp *loaderProvider) GetPrimitives() []schema.PrimitiveSchema {
	return lp.loader.GetPrimitives()
}

func (lp *loaderProvider) ValidateInput(primitiveName string, input map[string]interface{}) *taxonomy.TypedError {
	res := schema.ValidateInput(lp.loader.GetPrimitives(), primitiveName, input)
	if !res.Valid {
		return taxonomy.NewContractViolation("", primitiveName, res.InvalidFields)
	}
	return nil
}