// Package nostr is the small slice of the Nostr protocol the Buzz relay needs:
// keys, signed events (NIP-01), relay and HTTP authentication (NIP-42, NIP-98),
// owner attestations (NIP-OA) and bech32 key encoding (NIP-19).
//
// It is deliberately minimal and has no network code; the Buzz client builds on it.
// Signatures are BIP-340 Schnorr over secp256k1 (btcec).
package nostr

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

// Event is a NIP-01 event.
type Event struct {
	ID        string `json:"id"`
	PubKey    string `json:"pubkey"`
	CreatedAt int64  `json:"created_at"`
	Kind      int    `json:"kind"`
	Tags      Tags   `json:"tags"`
	Content   string `json:"content"`
	Sig       string `json:"sig"`
}

// Tags are an event's tags: each a name followed by values.
type Tags [][]string

// Find returns the first tag named name, or nil.
func (t Tags) Find(name string) []string {
	for _, tag := range t {
		if len(tag) > 0 && tag[0] == name {
			return tag
		}
	}
	return nil
}

// Value returns the first value of the first tag named name, or "".
func (t Tags) Value(name string) string {
	if tag := t.Find(name); len(tag) > 1 {
		return tag[1]
	}
	return ""
}

// Values returns the first value of every tag named name.
func (t Tags) Values(name string) []string {
	var out []string
	for _, tag := range t {
		if len(tag) > 1 && tag[0] == name {
			out = append(out, tag[1])
		}
	}
	return out
}

// Has reports whether a tag named name carries value as its first value.
func (t Tags) Has(name, value string) bool {
	for _, tag := range t {
		if len(tag) > 1 && tag[0] == name && tag[1] == value {
			return true
		}
	}
	return false
}

// Serialize is the NIP-01 canonical form whose SHA-256 is the event id:
// [0,pubkey,created_at,kind,tags,content], with no whitespace.
func (e *Event) Serialize() []byte {
	var b strings.Builder
	b.WriteString(`[0,"`)
	b.WriteString(e.PubKey)
	b.WriteString(`",`)
	b.WriteString(strconv.FormatInt(e.CreatedAt, 10))
	b.WriteByte(',')
	b.WriteString(strconv.Itoa(e.Kind))
	b.WriteString(",[")
	for i, tag := range e.Tags {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('[')
		for j, v := range tag {
			if j > 0 {
				b.WriteByte(',')
			}
			writeJSONString(&b, v)
		}
		b.WriteByte(']')
	}
	b.WriteString("],")
	writeJSONString(&b, e.Content)
	b.WriteByte(']')
	return []byte(b.String())
}

// writeJSONString writes s as a JSON string the way NIP-01 (and serde_json, which the
// Buzz relay verifies with) does: the short escapes for ", \, and the five common
// control characters, \u00xx for the other control characters, everything else
// verbatim. Go's encoding/json differs (it escapes <, >, & and U+2028/U+2029), which
// would change the id.
func writeJSONString(b *strings.Builder, s string) {
	const hexDigits = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}

// ComputeID returns the event's id (hex SHA-256 of its canonical form).
func (e *Event) ComputeID() string {
	sum := sha256.Sum256(e.Serialize())
	return hex.EncodeToString(sum[:])
}

// Sign sets the event's pubkey, id and signature. A zero CreatedAt is set to now.
func (e *Event) Sign(k *Keys) error {
	if k == nil {
		return errors.New("nostr: no key to sign with")
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = time.Now().Unix()
	}
	if e.Tags == nil {
		e.Tags = Tags{}
	}
	e.PubKey = k.PublicKey()
	e.ID = e.ComputeID()
	sig, err := signDigest(k, e.ID)
	if err != nil {
		return err
	}
	e.Sig = sig
	return nil
}

// signDigest signs a hex 32-byte digest, returning the hex signature.
func signDigest(k *Keys, digestHex string) (string, error) {
	digest, err := hex.DecodeString(digestHex)
	if err != nil || len(digest) != 32 {
		return "", errors.New("nostr: digest must be 32 bytes of hex")
	}
	sig, err := schnorr.Sign(k.sk, digest)
	if err != nil {
		return "", fmt.Errorf("nostr: sign: %w", err)
	}
	return hex.EncodeToString(sig.Serialize()), nil
}

// ErrBadEvent is returned by Verify for an event whose id or signature does not check out.
var ErrBadEvent = errors.New("nostr: event id or signature is invalid")

// Verify checks the event's id against its content and its signature against its pubkey.
func (e *Event) Verify() error {
	if e.ID == "" || e.ComputeID() != e.ID {
		return ErrBadEvent
	}
	return verifySig(e.PubKey, e.ID, e.Sig)
}

// verifySig checks a hex BIP-340 signature over a hex 32-byte message.
func verifySig(pubkeyHex, msgHex, sigHex string) error {
	pk, err := parsePubKey(pubkeyHex)
	if err != nil {
		return ErrBadEvent
	}
	msg, err := hex.DecodeString(msgHex)
	if err != nil || len(msg) != 32 {
		return ErrBadEvent
	}
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return ErrBadEvent
	}
	sig, err := schnorr.ParseSignature(sigBytes)
	if err != nil || !sig.Verify(msg, pk) {
		return ErrBadEvent
	}
	return nil
}
