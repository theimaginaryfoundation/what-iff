package nostr

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Event kinds used for authentication.
const (
	KindClientAuth = 22242 // NIP-42
	KindHTTPAuth   = 27235 // NIP-98
)

// AuthEvent is the NIP-42 kind 22242 event answering a relay's AUTH challenge.
// relayURL must be the URL the connection was opened on (Buzz binds it to the
// community's host). attestation, when not nil, is carried as the NIP-OA auth tag.
func AuthEvent(k *Keys, relayURL, challenge string, attestation *Attestation) (*Event, error) {
	ev := &Event{
		Kind: KindClientAuth,
		Tags: Tags{{"relay", relayURL}, {"challenge", challenge}},
	}
	if attestation != nil {
		ev.Tags = append(ev.Tags, attestation.Tag())
	}
	if err := ev.Sign(k); err != nil {
		return nil, err
	}
	return ev, nil
}

// HTTPAuthHeader is a NIP-98 Authorization header value ("Nostr <base64 event>")
// for one request. url must be the absolute URL the server expects (Buzz: the
// community's host and the request path, http or https). body, when not nil, is
// bound to the event with a payload hash.
//
// Each header carries a random nonce tag: two identical requests in the same second
// would otherwise sign the same event, and a relay's replay guard (Buzz has one)
// rejects the second as a replay.
func HTTPAuthHeader(k *Keys, method, url string, body []byte) (string, error) {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ev := &Event{
		Kind: KindHTTPAuth,
		Tags: Tags{{"u", url}, {"method", strings.ToUpper(method)}, {"nonce", hex.EncodeToString(nonce)}},
	}
	if body != nil {
		sum := sha256.Sum256(body)
		ev.Tags = append(ev.Tags, []string{"payload", hex.EncodeToString(sum[:])})
	}
	if err := ev.Sign(k); err != nil {
		return "", err
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	return "Nostr " + base64.StdEncoding.EncodeToString(raw), nil
}

// Attestation is a NIP-OA owner attestation: the owner's key authorising an agent
// key to publish under its own authorship, subject to conditions.
//
//	["auth", "<owner-pubkey-hex>", "<conditions>", "<sig-hex>"]
//	sig = BIP-340(SHA256("nostr:agent-auth:" || agent_pubkey_hex || ":" || conditions), owner_sk)
type Attestation struct {
	OwnerPubKey string
	Conditions  string
	Sig         string
}

// Tag is the attestation as an event tag.
func (a *Attestation) Tag() []string {
	return []string{"auth", a.OwnerPubKey, a.Conditions, a.Sig}
}

// JSON is the tag as the JSON array Buzz passes around (the x-auth-tag header,
// BUZZ_AUTH_TAG).
func (a *Attestation) JSON() string {
	raw, _ := json.Marshal(a.Tag())
	return string(raw)
}

var (
	// conditionClause is one NIP-OA clause: kind=N, created_at<N or created_at>N,
	// canonical decimals.
	conditionClause = regexp.MustCompile(`^(kind=|created_at<|created_at>)(0|[1-9][0-9]*)$`)
	lowerHex        = regexp.MustCompile(`^[0-9a-f]+$`)
)

// ParseAttestation reads an attestation pasted as its JSON tag
// (["auth", owner, conditions, sig]) and checks its shape. It does not check the
// signature; Verify does, against the agent key it is meant for.
func ParseAttestation(raw string) (*Attestation, error) {
	var tag []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &tag); err != nil {
		return nil, errors.New("the attestation must be the JSON auth tag: [\"auth\", owner, conditions, signature]")
	}
	if len(tag) != 4 || tag[0] != "auth" {
		return nil, errors.New("the attestation must have four parts and start with \"auth\"")
	}
	a := &Attestation{OwnerPubKey: tag[1], Conditions: tag[2], Sig: tag[3]}
	if len(a.OwnerPubKey) != 64 || !lowerHex.MatchString(a.OwnerPubKey) {
		return nil, errors.New("the attestation's owner key must be 64 lowercase hex characters")
	}
	if len(a.Sig) != 128 || !lowerHex.MatchString(a.Sig) {
		return nil, errors.New("the attestation's signature must be 128 lowercase hex characters")
	}
	if a.Conditions != "" {
		for _, c := range strings.Split(a.Conditions, "&") {
			if !conditionClause.MatchString(c) {
				return nil, fmt.Errorf("the attestation has an unsupported condition %q", c)
			}
		}
	}
	return a, nil
}

// ErrAttestationMismatch means the attestation was not signed for this agent key
// (or by its stated owner).
var ErrAttestationMismatch = errors.New("the attestation is not signed for this persona's key")

// attestationDigest is the hex SHA-256 the owner signs.
func attestationDigest(agentPubKey, conditions string) string {
	sum := sha256.Sum256([]byte("nostr:agent-auth:" + agentPubKey + ":" + conditions))
	return hex.EncodeToString(sum[:])
}

// Verify checks the attestation was signed by its owner for agentPubKey. Owner and
// agent must differ (self-attestation means nothing).
func (a *Attestation) Verify(agentPubKey string) error {
	if a.OwnerPubKey == agentPubKey {
		return errors.New("the attestation's owner is the persona itself")
	}
	if verifySig(a.OwnerPubKey, attestationDigest(agentPubKey, a.Conditions), a.Sig) != nil {
		return ErrAttestationMismatch
	}
	return nil
}

// ValidAt reports whether the attestation's created_at conditions allow an event
// created at t (kind conditions are the relay's to enforce per event).
func (a *Attestation) ValidAt(t time.Time) bool {
	if a.Conditions == "" {
		return true
	}
	at := t.Unix()
	for _, c := range strings.Split(a.Conditions, "&") {
		before, isBefore := strings.CutPrefix(c, "created_at<")
		after, isAfter := strings.CutPrefix(c, "created_at>")
		if !isBefore && !isAfter {
			continue
		}
		raw := before
		if isAfter {
			raw = after
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return false // a malformed bound allows nothing
		}
		if (isBefore && at >= n) || (isAfter && at <= n) {
			return false
		}
	}
	return true
}

// Attest signs an attestation with owner's key for agentPubKey. What Iff never holds
// an owner's key; this exists for tests and local rigs.
func Attest(owner *Keys, agentPubKey, conditions string) (*Attestation, error) {
	sig, err := signDigest(owner, attestationDigest(agentPubKey, conditions))
	if err != nil {
		return nil, err
	}
	return &Attestation{OwnerPubKey: owner.PublicKey(), Conditions: conditions, Sig: sig}, nil
}
