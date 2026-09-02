package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ha1tch/molu/pkg/catalogue"
	"github.com/ha1tch/molu/pkg/config"
	"github.com/ha1tch/molu/pkg/exec"
	"github.com/ha1tch/molu/pkg/health"
	"github.com/ha1tch/molu/pkg/mcp"
	"github.com/ha1tch/molu/pkg/obs"
	"github.com/ha1tch/molu/pkg/semantic"
)

func main() {
	var (
		transportFlag = flag.String("transport", "", "MCP transport mode: stdio or streamable-http")
		httpAddrFlag  = flag.String("http-addr", "", "HTTP listen address when in streamable-http mode")
		xoluURLFlag   = flag.String("xolu-url", "", "Substrate xolu base URL")
		hubURLFlag    = flag.String("hub-url", "", "Molu Hub base URL")
		tenantFlag    = flag.String("tenant", "", "Tenant identifier")
	)
	flag.Parse()

	// Load configuration from environment with defaults
	cfg := config.LoadFromEnv()
	if *transportFlag != "" {
		cfg.Transport = *transportFlag
	}
	if *httpAddrFlag != "" {
		cfg.HTTPAddr = *httpAddrFlag
	}
	if *xoluURLFlag != "" {
		cfg.XoluURL = *xoluURLFlag
	}
	if *hubURLFlag != "" {
		cfg.HubURL = *hubURLFlag
	}
	if *tenantFlag != "" {
		cfg.Tenant = *tenantFlag
	}

	// Initialize structured logging
	obs.InitLogger(cfg.LogLevel, cfg.LogFormat)
	slog.Info("Initializing Molu MCP Sidecar",
		"version", "0.2.0",
		"xolu_url", cfg.XoluURL,
		"hub_url", cfg.HubURL,
		"tenant", cfg.Tenant,
		"transport", cfg.Transport,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("Received termination signal, shutting down gracefully", "signal", sig.String())
		cancel()
	}()

	// 1. Initialize Semantic Map Store
	semanticStore := semantic.NewSemanticStore(cfg.Tenant)

	// 2. Initialize xolu Health Probe
	healthProbe := health.NewHealthProbe(
		cfg.XoluURL,
		cfg.PingInterval,
		cfg.PingTimeout,
		cfg.PongFreshness,
		cfg.PingFailFloor,
		cfg.PingFailCeiling,
	)

	// 3. Block until xolu answers its first successful pong. The MCP transport
	//    is not opened until this succeeds and the semantic map is populated —
	//    an agent connecting sooner would see an empty tool list (spec Part 2 §8.4).
	if err := healthProbe.WaitForStartup(ctx, cfg.StartupMaxAttempts); err != nil {
		if ctx.Err() != nil {
			slog.Info("Shutdown requested during startup wait; exiting")
			return
		}
		slog.Error("xolu did not become ready; molu is exiting", "error", err)
		os.Exit(1)
	}

	// 4. Start the background health probe now that a baseline pong exists.
	healthProbe.Start(ctx)

	// 5. Initialize Schema Poller and populate the semantic map once, before the
	//    transport opens, so the first tools/list already reflects the substrate.
	schemaPoller := semantic.NewSchemaPoller(
		cfg.XoluURL,
		cfg.XoluAuthMode,
		cfg.XoluToken,
		cfg.Tenant,
		cfg.SchemaPollInterval,
		semanticStore,
		healthProbe,
	)
	if err := schemaPoller.PollOnce(ctx); err != nil {
		slog.Warn("Initial semantic map poll failed; starting with an empty map", "error", err)
	}
	schemaPoller.Start(ctx)

	// 6. Initialize Hub Catalogue Store (if configured)
	var catStore *catalogue.CatalogueStore
	if cfg.HubURL != "" {
		catStore = catalogue.NewCatalogueStore(
			cfg.HubURL,
			cfg.HubAuthMode,
			cfg.HubToken,
			cfg.CataloguePollInterval,
		)
		catStore.Start(ctx)
	}

	// 7. Initialize Executor
	executor := exec.NewExecutor(
		cfg.XoluURL,
		cfg.XoluAuthMode,
		cfg.XoluToken,
		cfg.Tenant,
		cfg.CallTimeout,
		cfg.RedactPayloads,
		semanticStore,
		healthProbe,
	)

	// 8. Initialize MCP Server
	mcpServer := mcp.NewServer(executor, catStore)

	// 9. Start Transport
	if cfg.Transport == "streamable-http" {
		if err := mcpServer.RunHTTP(ctx, cfg.HTTPAddr, cfg.HTTPAuth, cfg.HTTPBearerToken); err != nil {
			slog.Error("MCP HTTP server encountered error", "error", err)
			os.Exit(1)
		}
	} else {
		if err := mcpServer.RunStdio(ctx); err != nil {
			slog.Error("MCP stdio server encountered error", "error", err)
			os.Exit(1)
		}
	}

	fmt.Fprintln(os.Stderr, "Molu MCP server stopped.")
}
