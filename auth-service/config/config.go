package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port string `env:"PORT" envDefault:"3001"`

	PostgresUser     string `env:"POSTGRES_USER" envDefault:"dramalist"`
	PostgresPassword string `env:"POSTGRES_PASSWORD,required"`
	PostgresHost     string `env:"POSTGRES_HOST" envDefault:"postgres"`
	PostgresPort     int    `env:"POSTGRES_PORT" envDefault:"5432"`

	RedisHost string `env:"REDIS_HOST" envDefault:"redis"`
	RedisPort int    `env:"REDIS_PORT" envDefault:"6379"`

	GoogleClientID     string `env:"GOOGLE_CLIENT_ID,required"`
	GoogleClientSecret string `env:"GOOGLE_CLIENT_SECRET,required"`
	GoogleRedirectURI  string `env:"GOOGLE_REDIRECT_URI,required"`

	GithubClientID     string `env:"GITHUB_CLIENT_ID,required"`
	GithubClientSecret string `env:"GITHUB_CLIENT_SECRET,required"`
	GithubRedirectURI  string `env:"GITHUB_REDIRECT_URI,required"`

	AppBaseURL         string `env:"APP_BASE_URL" envDefault:"http://localhost:8080"`
	AccessTokenTTL     int    `env:"JWT_ACCESS_TOKEN_TTL" envDefault:"900"`
	RefreshTokenTTL    int    `env:"JWT_REFRESH_TOKEN_TTL" envDefault:"2592000"`

	TOTPEncryptionKey string `env:"TOTP_ENCRYPTION_KEY,required"`

	KeysDir string `env:"KEYS_DIR" envDefault:"/keys"`
}

func (c *Config) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/auth_db?sslmode=disable",
		c.PostgresUser, c.PostgresPassword, c.PostgresHost, c.PostgresPort)
}

func (c *Config) RedisAddr() string {
	return fmt.Sprintf("%s:%d", c.RedisHost, c.RedisPort)
}

func (c *Config) IsProduction() bool {
	return len(c.AppBaseURL) >= 8 && c.AppBaseURL[:8] == "https://"
}

func Load() *Config {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}
	return cfg
}
