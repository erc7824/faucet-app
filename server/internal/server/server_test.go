package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"faucet-server/internal/clearnode"
	"faucet-server/internal/config"
	"faucet-server/internal/logger"
)

// mockClearnodeClient is a simple in-memory mock implementing ClearnodeClient.
type mockClearnodeClient struct {
	ownerAddress       string
	connErr            error
	operationalErr     error
	transferResult     *clearnode.TransferResult
	transferErr        error
	capturedDest       string
	capturedAsset      string
	capturedAmount     decimal.Decimal
}

func (m *mockClearnodeClient) GetOwnerAddress() string { return m.ownerAddress }
func (m *mockClearnodeClient) EnsureConnected() error  { return m.connErr }
func (m *mockClearnodeClient) EnsureOperational() error { return m.operationalErr }
func (m *mockClearnodeClient) Transfer(dest, asset string, amount decimal.Decimal) (*clearnode.TransferResult, error) {
	m.capturedDest = dest
	m.capturedAsset = asset
	m.capturedAmount = amount
	return m.transferResult, m.transferErr
}

func defaultConfig() *config.Config {
	return &config.Config{
		ServerPort:               "0",
		OwnerPrivateKey:          "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		ClearnodeURL:             "ws://localhost:0",
		TokenSymbol:              "usdc",
		StandardTipAmount:        "10",
		StandardTipAmountDecimal: decimal.RequireFromString("10"),
		LogLevel:                 "debug",
	}
}

func defaultMock() *mockClearnodeClient {
	return &mockClearnodeClient{
		ownerAddress: "0x9fc51BEE23Fb53569c46CcF013400f0E19524bd2",
		transferResult: &clearnode.TransferResult{
			TxID:   "tx-abc123",
			Amount: "10",
			Asset:  "usdc",
		},
	}
}

func TestMain(m *testing.M) {
	_ = logger.Initialize("debug")
	m.Run()
}

func TestRequestTokens_Success(t *testing.T) {
	mock := defaultMock()
	srv := NewServer(defaultConfig(), mock)

	testAddress := common.HexToAddress("0x742D35CC6634c0532925a3B8c17D18fBe3b78890").Hex()
	body, _ := json.Marshal(FaucetRequest{UserAddress: testAddress})

	req := httptest.NewRequest("POST", "/requestTokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp FaucetResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, MsgTokensSentSuccessfully, resp.Message)
	assert.Equal(t, "tx-abc123", resp.TxID)
	assert.Equal(t, "10", resp.Amount)
	assert.Equal(t, "usdc", resp.Asset)
	assert.Equal(t, testAddress, resp.Destination)

	assert.Equal(t, testAddress, mock.capturedDest)
	assert.Equal(t, "usdc", mock.capturedAsset)
	assert.True(t, decimal.RequireFromString("10").Equal(mock.capturedAmount))
}

func TestRequestTokens_InvalidAddress(t *testing.T) {
	srv := NewServer(defaultConfig(), defaultMock())

	body, _ := json.Marshal(FaucetRequest{UserAddress: "not-an-address"})
	req := httptest.NewRequest("POST", "/requestTokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrInvalidAddressFormat, resp.Error)
}

func TestRequestTokens_MissingField(t *testing.T) {
	srv := NewServer(defaultConfig(), defaultMock())

	body, _ := json.Marshal(map[string]string{"wrongField": "0x1234"})
	req := httptest.NewRequest("POST", "/requestTokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrInvalidRequestFormat, resp.Error)
}

func TestRequestTokens_ConnectionFailure(t *testing.T) {
	mock := defaultMock()
	mock.connErr = assert.AnError
	srv := NewServer(defaultConfig(), mock)

	body, _ := json.Marshal(FaucetRequest{UserAddress: "0x742d35Cc6634C0532925a3b8c17d18fBE3b78890"})
	req := httptest.NewRequest("POST", "/requestTokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrClearnodeConnectionFailed, resp.Error)
}

func TestRequestTokens_OperationalFailure(t *testing.T) {
	mock := defaultMock()
	mock.operationalErr = assert.AnError
	srv := NewServer(defaultConfig(), mock)

	body, _ := json.Marshal(FaucetRequest{UserAddress: "0x742d35Cc6634C0532925a3b8c17d18fBE3b78890"})
	req := httptest.NewRequest("POST", "/requestTokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrServiceUnavailable, resp.Error)
}

func TestRequestTokens_TransferFailure(t *testing.T) {
	mock := defaultMock()
	mock.transferResult = nil
	mock.transferErr = assert.AnError
	srv := NewServer(defaultConfig(), mock)

	body, _ := json.Marshal(FaucetRequest{UserAddress: "0x742d35Cc6634C0532925a3b8c17d18fBE3b78890"})
	req := httptest.NewRequest("POST", "/requestTokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrTransferFailed, resp.Error)
}

func TestInfoEndpoint(t *testing.T) {
	mock := defaultMock()
	srv := NewServer(defaultConfig(), mock)

	req := httptest.NewRequest("GET", "/info", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Nitrolite Faucet Server", resp["service"])
	assert.Equal(t, "1.0.0", resp["version"])
	assert.Equal(t, mock.ownerAddress, resp["faucet_address"])
	assert.Equal(t, "10", resp["standard_tip_amount"])
	assert.Equal(t, "usdc", resp["token_symbol"])
}
