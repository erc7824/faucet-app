package clearnode

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// Allowance represents an asset allowance for EIP-712 signing
type Allowance struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}

// EIP712Signer handles EIP-712 structured data signing for Clearnode authentication
type EIP712Signer struct {
	privateKey *ecdsa.PrivateKey
	address    common.Address
}

func NewEIP712Signer(privateKey *ecdsa.PrivateKey) *EIP712Signer {
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	return &EIP712Signer{
		privateKey: privateKey,
		address:    address,
	}
}

func (s *EIP712Signer) SignChallenge(
	challengeToken string,
	sessionKey common.Address,
	appName string,
	allowances []Allowance,
	scope string,
	application common.Address,
	expiresAt uint64,
) ([]byte, error) {
	// Convert allowances to the format expected by TypedData
	convertedAllowances := make([]map[string]interface{}, len(allowances))
	for i, allowance := range allowances {
		convertedAllowances[i] = map[string]interface{}{
			"asset":  allowance.Asset,
			"amount": allowance.Amount,
		}
	}

	// Create the EIP-712 typed data structure
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
			},
			"Policy": {
				{Name: "challenge", Type: "string"},
				{Name: "scope", Type: "string"},
				{Name: "wallet", Type: "address"},
				{Name: "session_key", Type: "address"},
				{Name: "expires_at", Type: "uint64"},
				{Name: "allowances", Type: "Allowance[]"},
			},
			"Allowance": {
				{Name: "asset", Type: "string"},
				{Name: "amount", Type: "string"},
			},
		},
		PrimaryType: "Policy",
		Domain: apitypes.TypedDataDomain{
			Name: appName,
		},
		Message: map[string]interface{}{
			"challenge":   challengeToken,
			"scope":       scope,
			"wallet":      s.address.Hex(),
			"session_key": sessionKey.Hex(),
			"expires_at":  new(big.Int).SetUint64(expiresAt),
			"allowances":  convertedAllowances,
		},
	}

	typedDataHash, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return nil, fmt.Errorf("failed to hash typed data: %w", err)
	}

	signature, err := crypto.Sign(typedDataHash, s.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign typed data hash: %w", err)
	}

	// Ensure the signature format is compatible with Clearnode expectations
	// Ethereum uses recovery ID 0/1, but some systems expect 27/28
	if signature[64] < 27 {
		signature[64] += 27
	}

	return signature, nil
}

func (s *EIP712Signer) GetAddress() common.Address {
	return s.address
}
