package main

import (
	"log"
	"time"

	"github.com/turbekoff/calcbot/pkg/env"
)

type Config struct {
	BotToken                string        `env:"CALCBOT_TELEGRAM_TOKEN,required"`
	BotOffset               int           `env:"CALCBOT_TELEGRAM_OFFSET" env-default:"20"`
	BotTimeout              int           `env:"CALCBOT_TELEGRAM_TIMEOUT" env-default:"60"`
	MemcachedTTLTimeout     time.Duration `env:"CALCBOT_MEMCACHED_TTL_TIMEOUT" env-default:"20m"`
	MemcachedCleanupTimeout time.Duration `env:"CALCBOT_MEMCACHED_CLEANUP_TIMEOUT" env-default:"1m"`
	ShutdownTimeout         time.Duration `env:"CALCBOT_SHUTDOWN_TIMEOUT" env-default:"2m"`
}

func LoadConfig() (*Config, error) {
	var cfg Config
	if err := env.Read(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("failed to load config, error: %v\n", err)
	}
}
