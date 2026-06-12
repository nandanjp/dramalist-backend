package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port string `env:"PORT" envDefault:"3002"`

	PostgresUser     string `env:"POSTGRES_USER" envDefault:"dramalist"`
	PostgresPassword string `env:"POSTGRES_PASSWORD,required"`
	PostgresHost     string `env:"POSTGRES_HOST" envDefault:"postgres"`
	PostgresPort     int    `env:"POSTGRES_PORT" envDefault:"5432"`

	RedisHost string `env:"REDIS_HOST" envDefault:"redis"`
	RedisPort int    `env:"REDIS_PORT" envDefault:"6379"`

	KafkaBootstrapServers string `env:"KAFKA_BOOTSTRAP_SERVERS" envDefault:"kafka:9092"`
	KafkaGroupID          string `env:"KAFKA_GROUP_ID"          envDefault:"user-service"`

	JWTSecret          string `env:"JWT_SECRET"            envDefault:""`
	ShowServiceURL     string `env:"SHOW_SERVICE_URL"      envDefault:"http://show-service.dramalist.svc.cluster.local:3003"`
	MinioPublicURL     string `env:"MINIO_PUBLIC_URL"      envDefault:"http://localhost:9000"`
	MinioProfileBucket string `env:"MINIO_PROFILE_BUCKET"  envDefault:"profiles"`
}

func (c *Config) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/user_db?sslmode=disable",
		c.PostgresUser, c.PostgresPassword, c.PostgresHost, c.PostgresPort)
}

func (c *Config) RedisAddr() string {
	return fmt.Sprintf("%s:%d", c.RedisHost, c.RedisPort)
}

func Load() *Config {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}
	return cfg
}
