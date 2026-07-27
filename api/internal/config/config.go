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
		Addr:          env("ATTESTA_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		TemplateDir:   env("ATTESTA_TEMPLATE_DIR", "templates"),
		OutputDir:     env("ATTESTA_OUTPUT_DIR", "generated/services"),
		APIToken:      os.Getenv("ATTESTA_API_TOKEN"),
		MaxBodyMB:     1,
		PrometheusURL: os.Getenv("ATTESTA_PROMETHEUS_URL"),
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
