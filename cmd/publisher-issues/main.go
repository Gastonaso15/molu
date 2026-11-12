// Command publisher-issues runs the Issue Tracker domain publisher: an HTTP
// service that registers issues.* in the Hub and executes them against xolu.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Gastonaso15/molu/pkg/publisher/issues"
)

// secret reads NAME_FILE (preferred) or NAME from the environment.
func secret(getenv func(string) string, name string) (string, error) {
	if path := getenv(name + "_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s_FILE: %w", name, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return getenv(name), nil
}

// publicURL is where the Execution Bridge reaches this service.
func publicURL(configured string, addr net.Addr) string {
	if configured != "" {
		return configured
	}
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String()
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func getWithFallback(getenv func(string) string, primary, fallback string) string {
	if v := getenv(primary); v != "" {
		return v
	}
	return getenv(fallback)
}

// run serves until ctx is cancelled. listening, if non-nil, receives the bound
// address once the listener is up (tests use it with a ":0" address).
func run(ctx context.Context, getenv func(string) string, listening func(addr string)) error {
	addr := getWithFallback(getenv, "MOLU_PUBLISHER_ISSUES_HTTP_ADDR", "MOLU_PUBLISHER_HTTP_ADDR")
	if addr == "" {
		addr = ":8093"
	}

	hubURL := getWithFallback(getenv, "MOLU_PUBLISHER_ISSUES_HUB_URL", "MOLU_PUBLISHER_HUB_URL")
	xoluURL := getWithFallback(getenv, "MOLU_PUBLISHER_ISSUES_XOLU_URL", "MOLU_PUBLISHER_XOLU_URL")

	apiToken, err := secret(getenv, "MOLU_PUBLISHER_ISSUES_API_TOKEN")
	if err != nil || apiToken == "" {
		apiToken, err = secret(getenv, "MOLU_PUBLISHER_API_TOKEN")
		if err != nil {
			return err
		}
	}

	xoluToken, err := secret(getenv, "MOLU_PUBLISHER_ISSUES_XOLU_TOKEN")
	if err != nil || xoluToken == "" {
		xoluToken, err = secret(getenv, "MOLU_PUBLISHER_XOLU_TOKEN")
		if err != nil {
			return err
		}
	}

	var missing []string
	if hubURL == "" {
		missing = append(missing, "MOLU_PUBLISHER_ISSUES_HUB_URL")
	}
	if xoluURL == "" {
		missing = append(missing, "MOLU_PUBLISHER_ISSUES_XOLU_URL")
	}
	if apiToken == "" {
		missing = append(missing, "MOLU_PUBLISHER_ISSUES_API_TOKEN (or _FILE)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}

	svc := issues.NewIssueService(issues.NewHTTPSubstrate(xoluURL, xoluToken, 10*time.Second))
	srv := &http.Server{
		Handler:      issues.NewServer(svc, apiToken).Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	if listening != nil {
		listening(ln.Addr().String())
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	pubURL := getWithFallback(getenv, "MOLU_PUBLISHER_ISSUES_PUBLIC_URL", "MOLU_PUBLISHER_PUBLIC_URL")
	registrar := issues.NewRegistrar(hubURL, publicURL(pubURL, ln.Addr()), issues.DefaultBackoff())
	registered := make(chan error, 1)
	go func() { registered <- registrar.Register(ctx) }()

	var runErr error
	select {
	case err := <-serveErr:
		runErr = fmt.Errorf("http server error: %w", err)
	case err := <-registered:
		if err != nil {
			runErr = fmt.Errorf("hub registration failed: %w", err)
			break
		}
		select {
		case err := <-serveErr:
			if err != nil {
				runErr = fmt.Errorf("http server error: %w", err)
			}
		case <-ctx.Done():
		}
	case <-ctx.Done():
	}

	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := registrar.Unregister(cleanup); err != nil && runErr == nil {
		fmt.Fprintf(os.Stderr, "publisher-issues: unregister: %v\n", err)
	}
	if err := srv.Shutdown(cleanup); err != nil && runErr == nil {
		runErr = fmt.Errorf("http server shutdown: %w", err)
	}
	return runErr
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, nil); err != nil {
		fmt.Fprintf(os.Stderr, "publisher-issues error: %v\n", err)
		os.Exit(1)
	}
}
