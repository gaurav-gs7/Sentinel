package config

import "os"

type Config struct {
	Addr          string
	DatabaseURL   string
	TemplateDir   string
	OutputDir     string
	APIToken      string
	MaxBodyMB     int64
	PrometheusURL string
}

func Load() Config {
	return Config{
		Addr:          env("SENTINEL_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		TemplateDir:   env("SENTINEL_TEMPLATE_DIR", "templates"),
		OutputDir:     env("SENTINEL_OUTPUT_DIR", "generated/services"),
		APIToken:      os.Getenv("SENTINEL_API_TOKEN"),
		MaxBodyMB:     1,
		PrometheusURL: os.Getenv("SENTINEL_PROMETHEUS_URL"),
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
