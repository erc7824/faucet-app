package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"faucet-server/internal/clearnode"
	"faucet-server/internal/config"
	"faucet-server/internal/logger"
)

// MockClearnodeServer represents a mock Clearnode WebSocket server
type MockClearnodeServer struct {
	server          *httptest.Server
	upgrader        websocket.Upgrader
	receivedMessage *clearnode.RPCMessage
	responseData    map[string]interface{}
	transferRequest *TransferCapture
}

// TransferCapture captures the transfer request parameters
type TransferCapture struct {
	Destination string
	Asset       string
	Amount      decimal.Decimal
	RequestID   uint64
}

func NewMockClearnodeServer() *MockClearnodeServer {
	mock := &MockClearnodeServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mock.server = httptest.NewServer(http.HandlerFunc(mock.handleWebSocket))
	return mock
}

func (m *MockClearnodeServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		var message clearnode.RPCMessage
		err := conn.ReadJSON(&message)
		if err != nil {
			break
		}

		m.receivedMessage = &message

		// Handle different request types
		if len(message.Req) >= 4 {
			requestID := message.Req[0]
			method := message.Req[1].(string)
			params := message.Req[2].(map[string]interface{})
			timestamp := message.Req[3]

			switch method {
			case "auth_request":
				m.sendAuthChallenge(conn, requestID, timestamp)
			case "auth_verify":
				m.sendAuthVerifyResponse(conn, requestID, timestamp)
			case "get_assets":
				m.sendAssetsResponse(conn, requestID, timestamp)
			case "get_ledger_balances":
				m.sendBalancesResponse(conn, requestID, timestamp)
			case "transfer":
				m.handleTransfer(conn, requestID, timestamp, params)
			}
		}
	}
}

func (m *MockClearnodeServer) sendAuthChallenge(conn *websocket.Conn, requestID, timestamp interface{}) {
	response := clearnode.RPCMessage{
		Res: []interface{}{
			requestID,
			"auth_challenge",
			map[string]interface{}{
				"challenge_message": "test-challenge-123",
			},
			timestamp,
		},
	}
	conn.WriteJSON(response)
}

func (m *MockClearnodeServer) sendAuthVerifyResponse(conn *websocket.Conn, requestID, timestamp interface{}) {
	response := clearnode.RPCMessage{
		Res: []interface{}{
			requestID,
			"auth_verify",
			map[string]interface{}{
				"success":   true,
				"jwt_token": "mock-jwt-token",
			},
			timestamp,
		},
	}
	conn.WriteJSON(response)
}

func (m *MockClearnodeServer) sendAssetsResponse(conn *websocket.Conn, requestID, timestamp interface{}) {
	response := clearnode.RPCMessage{
		Res: []interface{}{
			requestID,
			"get_assets",
			map[string]interface{}{
				"assets": []interface{}{
					map[string]interface{}{
						"token":    "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
						"symbol":   "usdc",
						"decimals": float64(6),
						"chain_id": float64(1),
					},
				},
			},
			timestamp,
		},
	}
	conn.WriteJSON(response)
}

func (m *MockClearnodeServer) sendBalancesResponse(conn *websocket.Conn, requestID, timestamp interface{}) {
	response := clearnode.RPCMessage{
		Res: []interface{}{
			requestID,
			"get_ledger_balances",
			map[string]interface{}{
				"ledger_balances": []interface{}{
					map[string]interface{}{
						"asset":  "usdc",
						"amount": "1000000000", // 1000 USDC with 6 decimals
					},
				},
			},
			timestamp,
		},
	}
	conn.WriteJSON(response)
}

func (m *MockClearnodeServer) handleTransfer(conn *websocket.Conn, requestID, timestamp interface{}, params map[string]interface{}) {
	// Capture transfer request details
	destination := params["destination"].(string)
	allocations := params["allocations"].([]interface{})
	allocation := allocations[0].(map[string]interface{})

	asset := allocation["asset"].(string)
	amountStr := allocation["amount"].(string)
	amount, _ := decimal.NewFromString(amountStr)

	m.transferRequest = &TransferCapture{
		Destination: destination,
		Asset:       asset,
		Amount:      amount,
		RequestID:   uint64(requestID.(float64)),
	}

	// Send successful transfer response
	response := clearnode.RPCMessage{
		Res: []interface{}{
			requestID,
			"transfer",
			map[string]interface{}{
				"transactions": []interface{}{
					map[string]interface{}{
						"id": "mock-tx-12345",
					},
				},
			},
			timestamp,
		},
	}
	conn.WriteJSON(response)
}

func (m *MockClearnodeServer) GetURL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

func (m *MockClearnodeServer) Close() {
	m.server.Close()
}

func (m *MockClearnodeServer) GetTransferRequest() *TransferCapture {
	return m.transferRequest
}

func TestFaucetServerIntegration(t *testing.T) {
	err := logger.Initialize("debug")
	require.NoError(t, err)

	mockClearnode := NewMockClearnodeServer()
	defer mockClearnode.Close()

	cfg := &config.Config{
		ServerPort:               "0", // Use random port
		OwnerPrivateKey:          "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		ClearnodeURL:             mockClearnode.GetURL(),
		TokenSymbol:              "usdc",
		StandardTipAmount:        "1000000", // 1 USDC with 6 decimals
		StandardTipAmountDecimal: decimal.RequireFromString("1000000"),
		LogLevel:                 "debug",
	}

	client, err := clearnode.NewClient(cfg.OwnerPrivateKey, cfg.ClearnodeURL)
	require.NoError(t, err)

	err = client.Connect()
	require.NoError(t, err)

	// Add small delay for connection to establish
	time.Sleep(100 * time.Millisecond)

	err = client.Authenticate()
	require.NoError(t, err)

	server := NewServer(cfg, client)

	t.Run("successful token request", func(t *testing.T) {
		testAddress := common.HexToAddress("0x742D35CC6634c0532925a3B8c17D18fBe3b78890").Hex() // this check-sums the address
		requestBody := FaucetRequest{
			UserAddress: testAddress,
		}
		jsonBody, err := json.Marshal(requestBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/requestTokens", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response FaucetResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify response structure
		assert.True(t, response.Success)
		assert.Equal(t, MsgTokensSentSuccessfully, response.Message)
		assert.Equal(t, "mock-tx-12345", response.TxID)
		assert.Equal(t, "1000000", response.Amount)
		assert.Equal(t, "usdc", response.Asset)
		assert.Equal(t, testAddress, response.Destination)

		// Verify transfer request sent to mock Clearnode
		transferReq := mockClearnode.GetTransferRequest()
		require.NotNil(t, transferReq)
		assert.Equal(t, testAddress, transferReq.Destination)
		assert.Equal(t, "usdc", transferReq.Asset)
		assert.True(t, decimal.RequireFromString("1000000").Equal(transferReq.Amount))
	})

	t.Run("invalid address format", func(t *testing.T) {
		requestBody := FaucetRequest{
			UserAddress: "invalid-address",
		}
		jsonBody, err := json.Marshal(requestBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/requestTokens", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var errorResponse ErrorResponse
		err = json.Unmarshal(w.Body.Bytes(), &errorResponse)
		require.NoError(t, err)
		assert.Equal(t, ErrInvalidAddressFormat, errorResponse.Error)
	})

	t.Run("missing userAddress field", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"wrongField": "0x742d35Cc6634C0532925a3b8c17d18fBE3b78890",
		}
		jsonBody, err := json.Marshal(requestBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/requestTokens", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var errorResponse ErrorResponse
		err = json.Unmarshal(w.Body.Bytes(), &errorResponse)
		require.NoError(t, err)
		assert.Equal(t, ErrInvalidRequestFormat, errorResponse.Error)
	})

	t.Run("health endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var healthResponse map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &healthResponse)
		require.NoError(t, err)
		assert.Equal(t, "healthy", healthResponse["status"])
		assert.Equal(t, true, healthResponse["connected"])
	})

	t.Run("info endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/info", nil)
		w := httptest.NewRecorder()

		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var infoResponse map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &infoResponse)
		require.NoError(t, err)
		assert.Equal(t, "Nitrolite Faucet Server", infoResponse["service"])
		assert.Equal(t, "1.0.0", infoResponse["version"])
		assert.Equal(t, "1000000", infoResponse["standard_tip_amount"])
		assert.Equal(t, "usdc", infoResponse["token_symbol"])
		assert.Contains(t, infoResponse["endpoints"], "/requestTokens")
	})
}
