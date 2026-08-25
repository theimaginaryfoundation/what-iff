package datastore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
)

func TestTokenCrypto_EncryptDecryptRoundTrip(t *testing.T) {
	crypto, err := newTokenCrypto("12345678901234567890123456789012")
	require.NoError(t, err)

	encrypted, err := crypto.Encrypt("token-123")
	require.NoError(t, err)
	require.NotEqual(t, "token-123", encrypted)
	require.Contains(t, encrypted, tokenCiphertextPrefix)

	decrypted, err := crypto.Decrypt(encrypted)
	require.NoError(t, err)
	require.Equal(t, "token-123", decrypted)
}

func TestTokenCrypto_DecryptInvalidCiphertext(t *testing.T) {
	crypto, err := newTokenCrypto("12345678901234567890123456789012")
	require.NoError(t, err)

	_, err = crypto.Decrypt("not-an-encrypted-token")
	require.Error(t, err)
}

func TestToMCPServerModel_DecryptFailureSetsErrorMessage(t *testing.T) {
	ds := &Datastore{
		tokenCrypto: nil, // Simulate missing/invalid crypto configuration for read.
	}

	row := &ent.MCPServer{
		ID:          uuid.New(),
		Name:        "stripe",
		Description: "Stripe MCP",
		ServerURL:   "https://mcp.stripe.com",
		AuthToken:   "ciphertext",
	}

	model := ds.toMCPServerModel(row)
	require.NotNil(t, model)
	require.Equal(t, "", model.AuthToken)
	require.NotEmpty(t, model.ErrorMessage)
}

func TestEncryptTokenForWrite_RequiresConfiguredSecret(t *testing.T) {
	ds := &Datastore{}
	_, err := ds.encryptTokenForWrite("token")
	require.ErrorIs(t, err, errTokenEncryptionSecretNotConfigured)
}

func TestValidateTokenEncryptionSecret_TooShort(t *testing.T) {
	_, err := ValidateTokenEncryptionSecret("short")
	require.ErrorIs(t, err, errTokenEncryptionSecretTooShort)
}
