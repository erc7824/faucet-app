# Nitrolite Faucet Server

A Go-based faucet server that distributes tokens through the Clearnode network using WebSocket connections.

## Features

- **WebSocket Integration**: Maintains persistent connection with Clearnode for real-time communication
- **Ethereum Wallet Integration**: Uses ECDSA private keys for authentication and signing
- **RESTful API**: Simple HTTP endpoints for token requests
- **Structured Logging**: JSON-formatted logs with configurable levels
- **Graceful Shutdown**: Proper cleanup of connections and resources
- **Address Validation**: Validates Ethereum addresses before processing requests

## Architecture

The application is structured into several packages:

- `internal/config`: Configuration management with environment variables
- `internal/logger`: Structured logging with logrus
- `internal/clearnode`: WebSocket client for Clearnode protocol
- `internal/server`: HTTP server with Gin framework

## Quick Start

1. **Clone and setup**:
   ```bash
   cd server
   go mod tidy
   ```

2. **Configure environment**:
   ```bash
   cp .env.example .env
   # Edit .env with your configuration
   ```

3. **Run the server**:
   ```bash
   go run main.go
   ```

## Configuration

The application uses [cleanenv](https://github.com/ilyakaznacheev/cleanenv) for configuration management. Configuration can be provided via:

1. **Environment variables** (highest priority)
2. **`.env` file** in the current directory
3. **Default values** for optional settings

Set the following environment variables (or create a `.env` file):

| Variable | Required | Default | Description | Example |
|----------|----------|---------|-------------|---------|
| `SERVER_PORT` | No | `8080` | HTTP server port | `8080` |
| `OWNER_PRIVATE_KEY` | **Yes** | - | Owner private key for auth (without 0x prefix) | `abcdef123...` |
| `SIGNER_PRIVATE_KEY` | **Yes** | - | Signer private key for transfers (without 0x prefix) | `fedcba098...` |
| `CLEARNODE_URL` | **Yes** | - | Clearnode WebSocket URL | `wss://testnet.clearnode.io/ws` |
| `TOKEN_SYMBOL` | **Yes** | - | Token symbol to distribute | `usdc` |
| `STANDARD_TIP_AMOUNT` | **Yes** | - | Amount to send per request (decimal format) | `10.0` |
| `LOG_LEVEL` | No | `info` | Logging level (debug/info/warn/error) | `info` |

## API Endpoints

### POST /requestTokens (or /getTokens)

Request tokens from the faucet.

**Request Body:**
```json
{
  "userAddress": "0x1234567890abcdef1234567890abcdef12345678"
}
```

**Success Response:**
```json
{
  "success": true,
  "message": "Tokens sent successfully",
  "txId": "12345",
  "amount": "1000000",
  "asset": "usdc",
  "destination": "0x1234567890abcdef1234567890abcdef12345678"
}
```

**Error Response:**
```json
{
  "error": "Invalid address format."
}
```

### GET /info

Service information endpoint.

**Response:**
```json
{
  "service": "Nitrolite Faucet Server",
  "version": "1.0.0",
  "faucet_address": "0xabcd...",
  "standard_tip_amount": "1000000",
  "token_symbol": "usdc",
  "endpoints": ["/requestTokens"]
}
```

## WebSocket Connection Management

The server maintains a persistent WebSocket connection with the Clearnode:

- **Connection**: Established on startup and maintained for the server's lifetime
- **Authentication**: Uses 3-step EIP-712 challenge-response authentication
- **EIP-712 Signing**: Implements structured data signing for secure authentication
- **Reconnection**: Currently manual (restart required if connection drops)
- **Message Handling**: Asynchronous request/response pattern with request ID tracking

### Key Separation Architecture

The faucet uses separate private keys for enhanced security:

- **Owner Private Key (`OWNER_PRIVATE_KEY`)**: Used for EIP-712 authentication with Clearnode
- **Signer Private Key (`SIGNER_PRIVATE_KEY`)**: Used for signing transfer transactions

**Security Benefits:**
- **Access Control**: Owner key controls authentication, signer key controls transfers
- **Key Rotation**: Signer key can be rotated without re-authentication
- **Reduced Risk**: Compromise of one key doesn't grant full access
- **Operational Flexibility**: Different keys for different operational roles

**Validation**: The system validates that both keys are different to prevent accidental reuse.

### EIP-712 Authentication Flow

1. **auth_request**: Server sends authentication request with **owner wallet address** and session parameters
2. **Challenge**: Clearnode responds with a random challenge token  
3. **EIP-712 Signing**: Server creates structured data signature using **owner private key**
4. **auth_verify**: Server sends the challenge with EIP-712 signature for verification
5. **JWT Token**: Upon successful verification, server receives JWT token for subsequent requests

The EIP-712 signature includes:
- Challenge token from server
- Owner wallet address and session key
- Application scope and permissions
- Expiration time
- Asset allowances (empty for faucet)

**Note**: Transfer transactions are signed with the **signer private key**, not the owner key.

## Startup Validation

The server performs comprehensive validation during startup to ensure reliable operation:

### Token Support Validation
- **Asset Discovery**: Queries Clearnode using `get_assets` to fetch all supported tokens
- **Symbol Verification**: Validates that the configured `TOKEN_SYMBOL` exists in supported assets
- **Early Failure**: Server refuses to start if the token is not supported

### Balance Verification  
- **Balance Check**: Queries faucet balance using `get_ledger_balances` after authentication
- **Minimum Threshold**: Requires balance ≥ 10,000 × tip amount for safe operation
- **Sufficient Funds**: Logs available balance and estimated number of possible transfers
- **Protective Shutdown**: Server refuses to start with insufficient funds to prevent failed requests

Example startup output:
```
INFO Successfully connected and authenticated with Clearnode
INFO Validating token support for: usdc  
INFO Token 'usdc' is supported by Clearnode
INFO Checking faucet balance
INFO Found usdc balance: 50000000 (required: 100000000 for 10,000 transfers)
INFO ✓ Sufficient usdc balance: 50000000.00 (enough for 5000 transfers)
INFO Faucet server is ready to serve requests
```

## Technical Implementation

### EIP-712 Structured Data Signing

The server implements EIP-712 (Ethereum Improvement Proposal 712) for secure authentication with Clearnode. This provides:

- **Type Safety**: Structured data format prevents signature replay attacks
- **Human Readable**: Clear indication of what is being signed
- **Domain Separation**: Signatures are bound to specific applications
- **Ethereum Standard**: Compatible with standard Ethereum wallets and tools

Key files:
- `internal/clearnode/eip712.go` - EIP-712 signing implementation
- `internal/clearnode/eip712_test.go` - Test suite for signing functionality
- `internal/clearnode/client.go` - Integration with Clearnode authentication

### Message Signing Flow

```go
typedData := apitypes.TypedData{
    Types: apitypes.Types{
        "EIP712Domain": {{"name", "string"}},
        "Policy": {
            {"challenge", "string"},
            {"scope", "string"}, 
            {"wallet", "address"},
            // ... other fields
        },
    },
    PrimaryType: "Policy",
    Domain: apitypes.TypedDataDomain{Name: appName},
    Message: map[string]interface{}{
        "challenge": challengeToken,
        "scope": scope,
        "wallet": walletAddress,
        // ... other parameters
    },
}
```

## Security Features

- **Key Separation**: Uses separate owner and signer private keys for enhanced security
- **Key Validation**: Enforces that owner and signer keys are different
- **Address Validation**: Validates Ethereum address format
- **Private Key Security**: Private keys are only used for signing, never exposed
- **CORS Support**: Configurable CORS headers for web integration
- **Request Signing**: All Clearnode requests are cryptographically signed
- **Role-Based Access**: Owner key for authentication, signer key for transfers

## Building for Production

```bash
# Build binary
go build -o faucet-server main.go

# Run with environment file
./faucet-server
```

## Docker Support

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o faucet-server main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/faucet-server .
CMD ["./faucet-server"]
```

## Development

```bash
# Install dependencies
go mod tidy

# Run with hot reload (using air)
go install github.com/cosmtrek/air@latest
air

# Run tests
go test ./...
```

## Logging

The application uses structured JSON logging:

```json
{
  "level": "info",
  "msg": "Processing faucet request for address: 0x1234...",
  "time": "2023-12-01T10:00:00Z"
}
```

Log levels: `debug`, `info`, `warn`, `error`, `fatal`

## Error Handling

- **Connection Errors**: Server returns 503 if Clearnode is unavailable
- **Validation Errors**: Returns 400 for invalid addresses or request format
- **Transfer Errors**: Returns 500 for Clearnode transfer failures
- **Timeout Handling**: 30-second timeout for Clearnode requests

## Monitoring

Key metrics to monitor:

- Transfer success/failure rates
- Response times
- Server resource usage

## Troubleshooting

**Connection Issues:**
- Verify `CLEARNODE_URL` is correct and accessible
- Check firewall settings for WebSocket connections
- Ensure private key has sufficient permissions

**Authentication Issues:**
- Verify private key format (no 0x prefix)
- Check Clearnode server authentication requirements
- Review logs for signature verification errors

**Transfer Issues:**
- Verify token address configuration
- Check faucet account balance
- Review Clearnode transfer logs
