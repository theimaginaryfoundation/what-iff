package nostr

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

// Keys is a secp256k1 keypair. The secret never leaves the value: there is no
// accessor that returns it except SecretHex, which the datastore uses to encrypt it.
type Keys struct {
	sk  *btcec.PrivateKey
	pub string
}

// GenerateKeys makes a new random keypair.
func GenerateKeys() (*Keys, error) {
	sk, err := btcec.NewPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("nostr: generate key: %w", err)
	}
	return keysFrom(sk), nil
}

// KeysFromHex loads a keypair from a 64-character hex secret key.
func KeysFromHex(secretHex string) (*Keys, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(secretHex))
	if err != nil || len(raw) != 32 {
		return nil, errors.New("nostr: secret key must be 32 bytes of hex")
	}
	sk, _ := btcec.PrivKeyFromBytes(raw)
	if sk.Key.IsZero() {
		return nil, errors.New("nostr: secret key is zero")
	}
	return keysFrom(sk), nil
}

func keysFrom(sk *btcec.PrivateKey) *Keys {
	return &Keys{sk: sk, pub: hex.EncodeToString(schnorr.SerializePubKey(sk.PubKey()))}
}

// PublicKey is the x-only public key, lowercase hex.
func (k *Keys) PublicKey() string { return k.pub }

// NPub is the public key as a NIP-19 npub.
func (k *Keys) NPub() string {
	s, _ := EncodeNPub(k.pub)
	return s
}

// SecretHex is the secret key as hex, for encrypting at rest. Never log or return it.
func (k *Keys) SecretHex() string {
	b := k.sk.Serialize()
	return hex.EncodeToString(b)
}

// parsePubKey parses a hex x-only public key.
func parsePubKey(pubkeyHex string) (*btcec.PublicKey, error) {
	raw, err := hex.DecodeString(pubkeyHex)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("nostr: public key must be 32 bytes of hex")
	}
	return schnorr.ParsePubKey(raw)
}

// NormalizePubKey accepts a public key as 64 hex characters or an npub and returns
// lowercase hex. It checks the key is a point on the curve.
func NormalizePubKey(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "nostr:")
	if strings.HasPrefix(strings.ToLower(s), "npub1") {
		h, err := DecodeNPub(s)
		if err != nil {
			return "", err
		}
		s = h
	}
	s = strings.ToLower(s)
	if _, err := parsePubKey(s); err != nil {
		return "", errors.New("nostr: not a valid public key")
	}
	return s, nil
}
