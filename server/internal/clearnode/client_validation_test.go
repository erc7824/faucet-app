package clearnode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClientValidation(t *testing.T) {
	t.Run("should fail when owner and signer keys are the same", func(t *testing.T) {
		sameKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
		mockURL := "ws://localhost:8080"

		client, err := NewClient(sameKey, sameKey, mockURL)

		assert.Nil(t, client)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner and signer private keys must be different for security reasons")
	})

	t.Run("should succeed when owner and signer keys are different", func(t *testing.T) {
		ownerKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
		signerKey := "fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"
		mockURL := "ws://localhost:8080"

		client, err := NewClient(ownerKey, signerKey, mockURL)

		assert.NotNil(t, client)
		require.NoError(t, err)

		// Verify addresses are different
		assert.NotEqual(t, client.ownerAddress, client.signerAddress)
		
		// Verify GetAddress returns signer address
		assert.Equal(t, client.signerAddress, client.GetAddress())
	})

	t.Run("should handle 0x prefixed keys correctly", func(t *testing.T) {
		ownerKey := "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
		signerKey := "0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"
		mockURL := "ws://localhost:8080"

		client, err := NewClient(ownerKey, signerKey, mockURL)

		assert.NotNil(t, client)
		require.NoError(t, err)
	})

	t.Run("should fail when same keys have different prefixes", func(t *testing.T) {
		baseKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
		ownerKey := "0x" + baseKey
		signerKey := baseKey // Same key without 0x prefix
		mockURL := "ws://localhost:8080"

		client, err := NewClient(ownerKey, signerKey, mockURL)

		assert.Nil(t, client)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner and signer private keys must be different for security reasons")
	})
}