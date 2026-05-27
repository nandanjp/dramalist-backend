package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port        string `env:"PORT"         envDefault:"3007"`
	OllamaURL   string `env:"OLLAMA_URL"   envDefault:"http://ollama:11434"`
	OllamaModel string `env:"OLLAMA_MODEL" envDefault:"llama3.2:3b"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}
