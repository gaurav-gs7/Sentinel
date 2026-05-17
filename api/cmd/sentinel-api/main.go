package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gauravgs7/sentinel/api/internal/catalog"
	"github.com/gauravgs7/sentinel/api/internal/config"
	"github.com/gauravgs7/sentinel/api/internal/httpapi"
	"github.com/gauravgs7/sentinel/api/internal/templates"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()
	store, closer, err := catalog.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open catalog store: %v", err)
	}
	defer closer()

	generator := templates.NewGenerator(cfg.TemplateDir, cfg.OutputDir)
	handler := httpapi.NewServer(store, generator,
		httpapi.WithAPIToken(cfg.APIToken),
		httpapi.WithMaxBodyBytes(cfg.MaxBodyMB*1024*1024),
		httpapi.WithPrometheusURL(cfg.PrometheusURL),
	)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("sentinel api listening on %s", cfg.Addr)
		errCh <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("graceful shutdown: %v", err)
	}
}
