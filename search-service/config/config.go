package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port                  string `env:"PORT"                    envDefault:"3005"`
	MeilisearchURL        string `env:"MEILISEARCH_URL"         envDefault:"http://meilisearch:7700"`
	KafkaBootstrapServers string `env:"KAFKA_BOOTSTRAP_SERVERS" envDefault:"kafka:9092"`
	KafkaGroupID          string `env:"KAFKA_GROUP_ID"          envDefault:"search-service"`
	ShowServiceURL        string `env:"SHOW_SERVICE_URL"        envDefault:"http://show-service:3003"`
	ReviewServiceURL      string `env:"REVIEW_SERVICE_URL"      envDefault:"http://review-service:3004"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}
