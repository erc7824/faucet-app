package config

import (
	"fmt"
	"strconv"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	ServerPort string `env:"SERVER_PORT" env-default:"8080" env-description:"HTTP server port"`

	OwnerPrivateKey   string `env:"OWNER_PRIVATE_KEY" env-required:"true" env-description:"Private key for faucet wallet (without 0x prefix)"`
	ClearnodeURL      string `env:"CLEARNODE_URL" env-required:"true" env-description:"Clearnode WebSocket URL"`
	TokenSymbol       string `env:"TOKEN_SYMBOL" env-required:"true" env-description:"Token symbol to distribute (e.g., usdc, weth)"`
	StandardTipAmount string `env:"STANDARD_TIP_AMOUNT" env-required:"true" env-description:"Default amount to send per request"`

	LogLevel string `env:"LOG_LEVEL" env-default:"info" env-description:"Logging level (debug, info, warn, error)"`
}

func Load() (*Config, error) {
	var config Config

	// Try to read from .env file first, then from environment variables
	if err := cleanenv.ReadConfig(".env", &config); err != nil {
		// If .env doesn't exist, try to read from environment variables only
		if err := cleanenv.ReadEnv(&config); err != nil {
			return nil, fmt.Errorf("failed to load configuration: %w", err)
		}
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

func (c *Config) Validate() error {
	if _, err := strconv.ParseFloat(c.StandardTipAmount, 64); err != nil {
		return fmt.Errorf("STANDARD_TIP_AMOUNT must be a valid number: %w", err)
	}

	return nil
}
