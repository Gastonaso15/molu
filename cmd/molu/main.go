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
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("Received signal, shutting down", "signal", sig.String())
		cancel()
	}()

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

	// §8.4: no seguimos hasta que xolu responda el primer pong.
	if err := probe.WaitReady(ctx); err != nil {
		if ctx.Err() == nil {
			slog.Error("xolu is not available, exiting", "error", err)
			os.Exit(1)
		}
	} else {
		go probe.Run(ctx)
	}

	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "Molu Frontend shutting down")
}