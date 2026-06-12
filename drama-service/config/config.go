package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port             string `env:"PORT"              envDefault:"3009"`
	PostgresUser     string `env:"POSTGRES_USER"     envDefault:"dramalist"`
	PostgresPassword string `env:"POSTGRES_PASSWORD,required"`
	PostgresHost     string `env:"POSTGRES_HOST"     envDefault:"postgres"`
	PostgresPort     int    `env:"POSTGRES_PORT"     envDefault:"5432"`
	MediaServiceURL  string `env:"MEDIA_SERVICE_URL" envDefault:"http://media-service.dramalist.svc.cluster.local:3006"`
	ShowServiceURL   string `env:"SHOW_SERVICE_URL"  envDefault:"http://show-service.dramalist.svc.cluster.local:3003"`
	RedisHost        string `env:"REDIS_HOST"        envDefault:"redis"`
	RedisPort        int    `env:"REDIS_PORT"        envDefault:"6379"`

	TMDBBaseURL      string `env:"TMDB_BASE_URL"       envDefault:"https://api.themoviedb.org"`
	TMDBAPIKey       string `env:"TMDB_API_KEY"        envDefault:""`
	TMDBImageBaseURL string `env:"TMDB_IMAGE_BASE_URL" envDefault:"https://image.tmdb.org/t/p/original"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) PostgresDSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/show_db?sslmode=disable",
		c.PostgresUser, c.PostgresPassword, c.PostgresHost, c.PostgresPort,
	)
}
