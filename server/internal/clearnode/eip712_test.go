package clearnode

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestEIP712Signer_SignChallenge(t *testing.T) {
	// Generate a test private key
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create EIP-712 signer
	signer := NewEIP712Signer(privateKey)

	// Test parameters
	challengeToken := "test-challenge-123"
	sessionKey := signer.GetAddress()
	appName := "Test App"
	allowances := []Allowance{
		{
			Asset:  "usdc",
			Amount: big.NewInt(1000000),
		},
	}
	scope := "app.transfer"
	application := common.Address{}
	expire := "3600000000"

	// Sign the challenge
	signature, err := signer.SignChallenge(
		challengeToken,
		sessionKey,
		appName,
		allowances,
		scope,
		application,
		expire,
	)

	if err != nil {
		t.Fatalf("Failed to sign challenge: %v", err)
	}

	// Verify signature length (65 bytes for ECDSA signature)
	if len(signature) != 65 {
		t.Errorf("Expected signature length 65, got %d", len(signature))
	}

	// Verify signature format (should have recovery ID 27 or 28)
	if signature[64] < 27 || signature[64] > 28 {
		t.Errorf("Invalid recovery ID: %d", signature[64])
	}

	t.Logf("Successfully generated EIP-712 signature with length %d", len(signature))
	t.Logf("Recovery ID: %d", signature[64])
}

func TestEIP712Signer_GetAddress(t *testing.T) {
	// Generate a test private key
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create EIP-712 signer
	signer := NewEIP712Signer(privateKey)

	// Verify address matches private key
	expectedAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	actualAddress := signer.GetAddress()

	if expectedAddress.Hex() != actualAddress.Hex() {
		t.Errorf("Address mismatch: expected %s, got %s", expectedAddress.Hex(), actualAddress.Hex())
	}
}
