package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ha1tch/molu/pkg/hub"
	"github.com/ha1tch/molu/pkg/obs"
)

func main() {
	var (
		addrFlag             = flag.String("addr", ":9080", "Hub HTTP listen address")
		tenantFlag           = flag.String("tenant", "default", "Tenant identifier served by this hub")
		heartbeatTimeoutFlag = flag.Duration("heartbeat-timeout", 90*time.Second, "Publisher silence before expiry")
		expiryIntervalFlag   = flag.Duration("expiry-interval", 15*time.Second, "Cadence to scan for expired publishers")
	)
	flag.Parse()

	obs.InitLogger("info", "console")
	slog.Info("Initializing Molu Hub Reference Implementation",
		"version", "0.2.0",
		"addr", *addrFlag,
		"tenant", *tenantFlag,
		"heartbeat_timeout", *heartbeatTimeoutFlag,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("Received termination signal, shutting down hub", "signal", sig.String())
		cancel()
	}()

	server := hub.NewHubServer(*addrFlag, *tenantFlag, *heartbeatTimeoutFlag, *expiryIntervalFlag)
	if err := server.Start(ctx); err != nil {
		slog.Error("Hub server error", "error", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "Molu Hub stopped.")
}
