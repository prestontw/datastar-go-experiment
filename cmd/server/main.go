package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/preston/go-datastar-patient-dashboard/internal/app"
	"github.com/preston/go-datastar-patient-dashboard/internal/postgres"
	"github.com/preston/go-datastar-patient-dashboard/internal/realtime"
	"github.com/preston/go-datastar-patient-dashboard/internal/security"
	webviews "github.com/preston/go-datastar-patient-dashboard/internal/web"
)

type config struct {
	address     string
	certificate string
	privateKey  string
	databaseURL string
	appSecret   string
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancelConnect := context.WithTimeout(rootCtx, 15*time.Second)
	defer cancelConnect()
	store, err := postgres.Open(connectCtx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(connectCtx); err != nil {
		return err
	}

	renderer, err := webviews.NewRenderer()
	if err != nil {
		return err
	}
	protector, err := security.New([]byte(cfg.appSecret))
	if err != nil {
		return fmt.Errorf("configure security: %w", err)
	}
	hub := realtime.NewHub()
	application := app.NewServer(store, renderer, hub, protector, logger)

	listenerErrors := make(chan error, 1)
	go func() {
		if err := postgres.Listen(rootCtx, cfg.databaseURL, logger, hub.Broadcast); err != nil && rootCtx.Err() == nil {
			listenerErrors <- err
		}
	}()

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	httpServer := &http.Server{
		Addr:              cfg.address,
		Handler:           application.Handler(),
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2", "http/1.1"},
		},
		BaseContext: func(_ net.Listener) context.Context { return rootCtx },
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("patient dashboard listening", "url", "https://localhost"+cfg.address, "http2", true)
		serverErrors <- httpServer.ListenAndServeTLS(cfg.certificate, cfg.privateKey)
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTPS: %w", err)
		}
		return nil
	case err := <-listenerErrors:
		return err
	case <-rootCtx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = httpServer.Close()
			return fmt.Errorf("shutdown HTTPS server: %w", err)
		}
		return nil
	}
}

func loadConfig() (config, error) {
	cfg := config{
		address:     envOr("ADDR", ":8443"),
		certificate: envOr("TLS_CERT_FILE", ".certs/localhost.pem"),
		privateKey:  envOr("TLS_KEY_FILE", ".certs/localhost-key.pem"),
		databaseURL: envOr("DATABASE_URL", "postgres://dashboard:dashboard@localhost:5432/patient_dashboard?sslmode=disable"),
		appSecret:   os.Getenv("APP_SECRET"),
	}
	if len(cfg.appSecret) < 32 {
		return config{}, errors.New("APP_SECRET is required and must contain at least 32 bytes")
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
