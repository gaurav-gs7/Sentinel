package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gauravgs7/sentinel/internal/controller"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var kubeconfig string
	var namespace string
	var resync time.Duration
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig; defaults to in-cluster config, then ~/.kube/config")
	flag.StringVar(&namespace, "namespace", "", "namespace to watch; empty watches all namespaces")
	flag.DurationVar(&resync, "resync", 30*time.Second, "periodic RolloutGuard resync interval")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := kubeConfig(kubeconfig)
	if err != nil {
		logger.Error("load Kubernetes config", "error", err)
		os.Exit(1)
	}

	ctrl, err := controller.NewRolloutGuardController(cfg, controller.Options{
		Namespace: namespace,
		Resync:    resync,
		Logger:    logger,
	})
	if err != nil {
		logger.Error("create rollout guard controller", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := ctrl.Run(ctx, 2); err != nil {
		logger.Error("run rollout guard controller", "error", err)
		os.Exit(1)
	}
}

func kubeConfig(path string) (*rest.Config, error) {
	if path != "" {
		return clientcmd.BuildConfigFromFlags("", path)
	}
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
}
