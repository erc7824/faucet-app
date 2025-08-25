package clearnode

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"

	"faucet-server/internal/logger"
)

const RESPONSE_TIMEOUT_SEC = 5

type Client struct {
	privateKey *ecdsa.PrivateKey
	address    common.Address
	url        string

	conn        *websocket.Conn
	isConnected atomic.Bool
	jwtToken    string
	lastReqID   atomic.Uint64
	mu          sync.RWMutex

	// EIP-712 signer for authentication
	eip712Signer *EIP712Signer

	// Response handling
	pendingRequests map[uint64]chan *RPCResponse
	responseMu      sync.RWMutex
}

type RPCMessage struct {
	Req []interface{} `json:"req,omitempty"`
	Res []interface{} `json:"res,omitempty"`
	Sid string        `json:"sid,omitempty"`
	Sig []string      `json:"sig"`
}

type RPCResponse struct {
	RequestID uint64                 `json:"request_id"`
	Method    string                 `json:"method"`
	Data      map[string]interface{} `json:"data"`
	Timestamp uint64                 `json:"timestamp"`
}

type TransferRequest struct {
	Destination string       `json:"destination"`
	Allocations []Allocation `json:"allocations"`
}

type Allocation struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}

func NewClient(privateKeyHex, clearnodeURL string) (*Client, error) {
	// Clean the private key (remove 0x prefix if present)
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	eip712Signer := NewEIP712Signer(privateKey)

	return &Client{
		privateKey:      privateKey,
		address:         address,
		url:             clearnodeURL,
		eip712Signer:    eip712Signer,
		pendingRequests: make(map[uint64]chan *RPCResponse),
	}, nil
}

func (c *Client) Connect() error {
	logger.Infof("Connecting to Clearnode at %s", c.url)

	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	c.conn = conn
	c.isConnected.Store(true)

	// Start listening for responses
	go c.listenForResponses()

	logger.Info("WebSocket connection established")
	return nil
}

func (c *Client) Authenticate() error {
	logger.Info("Starting authentication flow")

	// Authentication parameters
	appName := "Nitrolite Faucet"
	scope := "app.transfer"
	expire := "36000000"            // 10_000 hours
	sessionKey := c.address         // Use same address as session key for simplicity
	application := common.Address{} // Zero address if no specific app

	// Step 1: Send auth_request
	authRequestData := map[string]interface{}{
		"address":     c.address.Hex(),
		"session_key": sessionKey.Hex(),
		"app_name":    appName,
		"scope":       scope,
		"expire":      expire,
		"application": application.Hex(),
		"allowances":  []map[string]interface{}{}, // Empty allowances for faucet
	}

	challengeResponse, err := c.sendRequest("auth_request", authRequestData)
	if err != nil {
		return fmt.Errorf("auth_request failed: %w", err)
	}

	challengeMessage, ok := challengeResponse.Data["challenge_message"].(string)
	if !ok {
		return fmt.Errorf("invalid challenge response format")
	}

	logger.Debugf("Received challenge: %s", challengeMessage)

	// Step 2: Sign the challenge using EIP-712
	allowances := []Allowance{} // Empty allowances for faucet
	signature, err := c.eip712Signer.SignChallenge(
		challengeMessage,
		sessionKey,
		appName,
		allowances,
		scope,
		application,
		expire,
	)
	if err != nil {
		return fmt.Errorf("failed to sign challenge: %w", err)
	}

	signatureHex := hexutil.Encode(signature)
	logger.Debugf("Generated EIP-712 signature: %s", signatureHex[:10]+"...")

	// Step 3: Send auth_verify with the EIP-712 signature
	verifyData := map[string]interface{}{
		"challenge": challengeMessage,
	}

	requestID := c.lastReqID.Add(1)
	timestamp := uint64(time.Now().UnixMilli())
	req := []interface{}{requestID, "auth_verify", verifyData, timestamp}

	message := RPCMessage{
		Req: req,
		Sig: []string{signatureHex},
	}

	// Create response channel
	responseChan := make(chan *RPCResponse, 1)
	c.responseMu.Lock()
	c.pendingRequests[requestID] = responseChan
	c.responseMu.Unlock()

	// Send the message
	c.mu.Lock()
	err = c.conn.WriteJSON(message)
	c.mu.Unlock()

	if err != nil {
		c.responseMu.Lock()
		delete(c.pendingRequests, requestID)
		c.responseMu.Unlock()
		return fmt.Errorf("failed to send auth_verify: %w", err)
	}

	logger.Debugf("Sent auth_verify with EIP-712 signature. Waiting for response...")

	// Wait for response
	select {
	case verifyResponse := <-responseChan:
		if verifyResponse.Method == "error" {
			errorMsg, _ := verifyResponse.Data["error"].(string)
			return fmt.Errorf("auth_verify error: %s", errorMsg)
		}

		success, ok := verifyResponse.Data["success"].(bool)
		if !ok || !success {
			return fmt.Errorf("authentication failed. Response does not include success: %v", verifyResponse.Data)
		}

		jwtToken, ok := verifyResponse.Data["jwt_token"].(string)
		if ok {
			c.jwtToken = jwtToken
			logger.Debug("JWT token received and stored")
		}

		logger.Info("Authentication successful")
		return nil

	case <-time.After(RESPONSE_TIMEOUT_SEC * time.Second):
		c.responseMu.Lock()
		delete(c.pendingRequests, requestID)
		c.responseMu.Unlock()
		return fmt.Errorf("auth_verify timeout")
	}
}

func (c *Client) Transfer(destination, asset, amount string) (*RPCResponse, error) {
	if !c.isConnected.Load() {
		return nil, fmt.Errorf("client is not connected")
	}

	transferData := TransferRequest{
		Destination: destination,
		Allocations: []Allocation{
			{
				Asset:  asset,
				Amount: amount,
			},
		},
	}

	logger.Infof("Sending transfer: %s %s to %s", amount, asset, destination)

	response, err := c.sendRequest("transfer", transferData)
	if err != nil {
		return nil, fmt.Errorf("transfer failed: %w", err)
	}

	logger.Info("Transfer completed successfully", "destination", destination)
	return response, nil
}

func (c *Client) sendRequest(method string, params interface{}) (*RPCResponse, error) {
	requestID := c.lastReqID.Add(1)
	timestamp := uint64(time.Now().UnixMilli())

	req := []interface{}{requestID, method, params, timestamp}

	signature, err := c.signMessage(req)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message: %w", err)
	}

	message := RPCMessage{
		Req: req,
		Sig: []string{signature},
	}

	responseChan := make(chan *RPCResponse, 1)
	c.responseMu.Lock()
	c.pendingRequests[requestID] = responseChan
	c.responseMu.Unlock()

	c.mu.Lock()
	err = c.conn.WriteJSON(message)
	c.mu.Unlock()

	if err != nil {
		c.responseMu.Lock()
		delete(c.pendingRequests, requestID)
		c.responseMu.Unlock()
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	logger.Debugf("Sent request %d: %s", requestID, method)

	select {
	case response := <-responseChan:
		return response, nil
	case <-time.After(RESPONSE_TIMEOUT_SEC * time.Second):
		c.responseMu.Lock()
		delete(c.pendingRequests, requestID)
		c.responseMu.Unlock()
		return nil, fmt.Errorf("request timeout")
	}
}

func (c *Client) listenForResponses() {
	defer func() {
		c.isConnected.Store(false)
		if c.conn != nil {
			c.conn.Close()
		}
	}()

	for {
		var message RPCMessage
		err := c.conn.ReadJSON(&message)
		if err != nil {
			logger.Errorf("Failed to read WebSocket message: %v", err)
			break
		}

		if len(message.Res) >= 4 {
			requestID, ok := message.Res[0].(float64)
			if !ok {
				logger.Warn("Invalid response format: missing request ID")
				continue
			}

			method, ok := message.Res[1].(string)
			if !ok {
				logger.Warn("Invalid response format: missing method")
				continue
			}

			data, ok := message.Res[2].(map[string]interface{})
			if !ok {
				logger.Warn("Invalid response format: missing data")
				continue
			}

			timestamp, ok := message.Res[3].(float64)
			if !ok {
				logger.Warn("Invalid response format: missing timestamp")
				continue
			}

			response := &RPCResponse{
				RequestID: uint64(requestID),
				Method:    method,
				Data:      data,
				Timestamp: uint64(timestamp),
			}

			logger.Debugf("Received response %d: %s", response.RequestID, response.Method)

			// Check for error responses
			if method == "error" {
				errorMsg, ok := data["error"].(string)
				if ok {
					logger.Errorf("Server error for request %d: %s", response.RequestID, errorMsg)
				}
			}

			// Send to waiting request
			c.responseMu.RLock()
			if ch, exists := c.pendingRequests[response.RequestID]; exists {
				select {
				case ch <- response:
				default:
				}
			}
			c.responseMu.RUnlock()

			// Clean up
			c.responseMu.Lock()
			delete(c.pendingRequests, response.RequestID)
			c.responseMu.Unlock()
		}
	}
}

func (c *Client) signMessage(data interface{}) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal data: %w", err)
	}

	hash := crypto.Keccak256Hash(jsonData)
	signature, err := crypto.Sign(hash.Bytes(), c.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign: %w", err)
	}

	return hexutil.Encode(signature), nil
}

func (c *Client) Close() error {
	c.isConnected.Store(false)
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) IsConnected() bool {
	return c.isConnected.Load()
}

func (c *Client) GetAddress() common.Address {
	return c.address
}
