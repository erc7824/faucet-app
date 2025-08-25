package server

import (
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"

	"faucet-server/internal/clearnode"
	"faucet-server/internal/config"
	"faucet-server/internal/logger"
)

type Server struct {
	config          *config.Config
	clearnodeClient *clearnode.Client
	router          *gin.Engine
}

type FaucetRequest struct {
	UserAddress string `json:"userAddress" binding:"required"`
}

type FaucetResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
	TxID        string `json:"txId,omitempty"`
	Amount      string `json:"amount,omitempty"`
	Asset       string `json:"asset,omitempty"`
	Destination string `json:"destination,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewServer(cfg *config.Config, client *clearnode.Client) *Server {
	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Add middleware
	router.Use(gin.Recovery())
	router.Use(requestLogger())
	router.Use(corsMiddleware())

	server := &Server{
		config:          cfg,
		clearnodeClient: client,
		router:          router,
	}

	server.setupRoutes()
	return server
}

func (s *Server) setupRoutes() {
	s.router.GET("/health", s.healthCheck)
	s.router.POST("/requestTokens", s.requestTokens)
	s.router.GET("/info", s.getInfo)
}

func (s *Server) healthCheck(c *gin.Context) {
	status := "healthy"
	if !s.clearnodeClient.IsConnected() {
		status = "unhealthy - no clearnode connection"
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":    status,
			"connected": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    status,
		"connected": true,
	})
}

func (s *Server) getInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service":             "Nitrolite Faucet Server",
		"version":             "1.0.0",
		"faucet_address":      s.clearnodeClient.GetAddress(),
		"standard_tip_amount": s.config.StandardTipAmount,
		"token_address":       s.config.TokenAddress,
		"endpoints":           []string{"/requestTokens"},
	})
}

func (s *Server) requestTokens(c *gin.Context) {
	var req FaucetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warnf("Invalid request format: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request format. Expected JSON with 'userAddress' field.",
		})
		return
	}

	// Validate the user address
	userAddress := strings.TrimSpace(req.UserAddress)
	if !common.IsHexAddress(userAddress) {
		logger.Warnf("Invalid Ethereum address: %s", userAddress)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid Ethereum address format.",
		})
		return
	}

	userAddress = common.HexToAddress(userAddress).Hex()

	logger.Infof("Processing faucet request for address: %s", userAddress)

	if !s.clearnodeClient.IsConnected() {
		logger.Error("Clearnode client is not connected")
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error: "Faucet service is currently unavailable. Please try again later.",
		})
		return
	}

	// TODO: replace `tokenAddress` with `asset` name and check whether it is approved by the Clearnode on the startup
	asset := s.getAssetName(s.config.TokenAddress)

	response, err := s.clearnodeClient.Transfer(
		userAddress,
		asset,
		s.config.StandardTipAmount,
	)
	if err != nil {
		logger.Errorf("Transfer failed for %s: %v", userAddress, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Failed to send tokens. Please try again later.",
		})
		return
	}

	// Extract transaction ID from response if available
	txID := ""
	if transactions, ok := response.Data["transactions"].([]interface{}); ok && len(transactions) > 0 {
		if tx, ok := transactions[0].(map[string]interface{}); ok {
			if id, ok := tx["id"].(string); ok {
				txID = id
			}
		}
	}

	logger.Infof("Successfully sent %s %s to %s (txID: %s)",
		s.config.StandardTipAmount, asset, userAddress, txID)

	c.JSON(http.StatusOK, FaucetResponse{
		Success:     true,
		Message:     "Tokens sent successfully",
		TxID:        txID,
		Amount:      s.config.StandardTipAmount,
		Asset:       asset,
		Destination: userAddress,
	})
}

func (s *Server) getAssetName(tokenAddress string) string {
	// This is a simplified mapping. In a production system,
	// you might want to query the Clearnode for supported assets
	// or maintain a more comprehensive mapping.
	switch strings.ToLower(tokenAddress) {
	case "0xa0b86a33e6ba77b8f16c0d15b9a11d2fe7f0c5e5": // Example USDC address
		return "usdc"
	case "0x4200000000000000000000000000000000000006": // Example WETH address
		return "weth"
	default:
		// Default to a generic token name or the address itself
		return "token"
	}
}

func (s *Server) Start() error {
	addr := ":" + s.config.ServerPort
	logger.Infof("Starting HTTP server on port %s", s.config.ServerPort)
	return s.router.Run(addr)
}

// Middleware functions

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Log request
		logger.Debugf("%s %s from %s", c.Request.Method, c.Request.URL.Path, c.ClientIP())
		c.Next()
		// Log response status
		logger.Debugf("%s %s - %d", c.Request.Method, c.Request.URL.Path, c.Writer.Status())
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
