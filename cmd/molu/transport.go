package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Gastonaso15/molu/pkg/config"
	"github.com/Gastonaso15/molu/pkg/mcp"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

const httpShutdownTimeout = 5 * time.Second

// serveTransport runs the MCP server on the configured transport and returns
// when ctx is cancelled (nil) or the transport fails (error). stdio reads
// os.Stdin; streamable-http listens on cfg.HTTPAddr and shuts down gracefully.
func serveTransport(ctx context.Context, srv *mcp.Server, cfg *config.Config, logger telemetry.Logger) error {
	if cfg.Transport != "streamable-http" {
		return srv.ServeStdio(ctx)
	}
	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("streamable-http: listen on %s: %w", cfg.HTTPAddr, err)
	}
	return serveHTTP(ctx, ln, srv, cfg, logger)
}

func serveHTTP(ctx context.Context, ln net.Listener, srv *mcp.Server, cfg *config.Config, logger telemetry.Logger) error {
	httpCfg := mcp.HTTPConfig{Auth: cfg.HTTPAuth}
	if cfg.HTTPAuth == "bearer" {
		raw, err := os.ReadFile(cfg.HTTPBearerTokenFile)
		if err != nil {
			return fmt.Errorf("streamable-http: read bearer token file: %w", err)
		}
		httpCfg.BearerToken = strings.TrimSpace(string(raw))
	}
	handler, err := mcp.NewHTTPHandler(srv, httpCfg)
	if err != nil {
		return err
	}

	// No WriteTimeout: GET streams are long-lived by design.
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(ln) }()
	logger.System().Info().Str("addr", ln.Addr().String()).Str("auth", cfg.HTTPAuth).Msg("MCP streamable-http server listening")

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	// Open GET streams never finish on their own: end them first so Shutdown
	// does not wait out its whole timeout.
	handler.Close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("streamable-http: shutdown: %w", err)
	}
	return nil
}
