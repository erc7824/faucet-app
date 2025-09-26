package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"faucet-server/internal/clearnode"
	"faucet-server/internal/config"
	"faucet-server/internal/logger"
	"faucet-server/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	if err := logger.Initialize(cfg.LogLevel); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	logger.Info("Starting Nitrolite Faucet Server")
	logger.Infof("Configuration loaded: Server port=%s, Clearnode URL=%s",
		cfg.ServerPort, cfg.ClearnodeURL)

	client, err := clearnode.NewClient(cfg.OwnerPrivateKey, cfg.SignerPrivateKey, cfg.ClearnodeURL, cfg.TokenSymbol, cfg.StandardTipAmountDecimal, cfg.MinTransferCount)
	if err != nil {
		logger.Fatalf("Failed to create Clearnode client: %v", err)
	}

	logger.Infof("Faucet signer address: %s", client.GetAddress())

	if err := client.Connect(); err != nil {
		logger.Fatalf("Failed to connect to Clearnode: %v", err)
	}

	if err := client.Authenticate(); err != nil {
		logger.Fatalf("Failed to authenticate with Clearnode: %v", err)
	}

	logger.Info("Successfully connected and authenticated with Clearnode")

	if err := client.EnsureOperational(); err != nil {
		logger.Fatalf("Operational check failed: %v", err)
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
