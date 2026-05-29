package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port string `env:"PORT" envDefault:"3006"`

	PostgresUser     string `env:"POSTGRES_USER" envDefault:"dramalist"`
	PostgresPassword string `env:"POSTGRES_PASSWORD,required"`
	PostgresHost     string `env:"POSTGRES_HOST" envDefault:"postgres"`
	PostgresPort     int    `env:"POSTGRES_PORT" envDefault:"5432"`

	MinioEndpoint  string `env:"MINIO_ENDPOINT" envDefault:"minio:9000"`
	MinioAccessKey string `env:"MINIO_ACCESS_KEY" envDefault:"dramalist"`
	MinioSecretKey string `env:"MINIO_SECRET_KEY,required"`
}

func (c *Config) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/media_db?sslmode=disable",
		c.PostgresUser, c.PostgresPassword, c.PostgresHost, c.PostgresPort)
}

func Load() *Config {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}
	return cfg
}
