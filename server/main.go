package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"faucet-server/internal/clearnode"
	"faucet-server/internal/config"
	"faucet-server/internal/logger"
	"faucet-server/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	if err := logger.Initialize(cfg.LogLevel); err != nil {
		logger.Fatalf("Failed to initialize logger: %v", err)
	}

	logger.Info("Starting Nitrolite Faucet Server")
	logger.Infof("Configuration loaded: Server port=%s, Clearnode URL=%s",
		cfg.ServerPort, cfg.ClearnodeURL)

	client, err := clearnode.NewClient(cfg.OwnerPrivateKey, cfg.ClearnodeURL)
	if err != nil {
		logger.Fatalf("Failed to create Clearnode client: %v", err)
	}

	logger.Infof("Faucet owner address: %s", client.GetAddress())

	if err := client.Connect(); err != nil {
		logger.Fatalf("Failed to connect to Clearnode: %v", err)
	}

	if err := client.Authenticate(); err != nil {
		logger.Fatalf("Failed to authenticate with Clearnode: %v", err)
	}

	logger.Info("Successfully connected and authenticated with Clearnode")

	if err := validateTokenSupport(client, cfg.TokenSymbol); err != nil {
		logger.Fatalf("Token validation failed: %v", err)
	}

	if err := checkFaucetBalance(client, cfg.TokenSymbol, cfg.StandardTipAmount); err != nil {
		logger.Fatalf("Balance check failed: %v", err)
	}

	httpServer := server.NewServer(cfg, client)

	go func() {
		if err := httpServer.Start(); err != nil {
			logger.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	logger.Info("Faucet server is ready to serve requests")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	if err := client.Close(); err != nil {
		logger.Errorf("Error closing Clearnode connection: %v", err)
	}

	logger.Info("Server shutdown complete")
}

func validateTokenSupport(client *clearnode.Client, tokenSymbol string) error {
	logger.Debugf("Validating token support for: %s", tokenSymbol)

	assets, err := client.GetAssets()
	if err != nil {
		return fmt.Errorf("failed to fetch supported assets: %w", err)
	}

	for _, asset := range assets {
		if asset.Symbol == tokenSymbol {
			logger.Debugf("Token '%s' is supported by Clearnode (address: %s, decimals: %d)",
				tokenSymbol, asset.Token, asset.Decimals)
			return nil
		}
	}

	return fmt.Errorf("token '%s' is not supported by Clearnode", tokenSymbol)
}

func checkFaucetBalance(client *clearnode.Client, tokenSymbol string, standardTipAmount string) error {
	logger.Debug("Checking faucet balance")

	balance, err := client.GetFaucetBalance(tokenSymbol)
	if err != nil {
		return fmt.Errorf("failed to fetch faucet balance: %w", err)
	}

	tipAmount, err := strconv.ParseFloat(standardTipAmount, 64)
	if err != nil {
		return fmt.Errorf("invalid tip amount format: %w", err)
	}

	amount, err := strconv.ParseFloat(balance.Amount, 64)
	if err != nil {
		return fmt.Errorf("failed to parse balance amount for %s: %w", tokenSymbol, err)
	}

	minTransferCount := 10000.0
	minRequiredBalance := tipAmount * minTransferCount

	if amount < minRequiredBalance {
		return fmt.Errorf("insufficient %s balance: %.2f (required: %.0f for 10,000 transfers)",
			tokenSymbol, amount, minRequiredBalance)
	}

	logger.Infof("✓ Sufficient %s balance: %.2f",
		tokenSymbol, amount)
	return nil
}
