package nostr

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The NIP-19 spec's example key pair, so the public key derivation is checked against a
// published vector rather than this package's own output.
const (
	buzzSecret = "67dea2ed018072d675f5415ecfaed7d2597555e202d85b3d65ea4e58d2d92ffa"
	buzzPublic = "7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e"
)

func TestKeysFromHexMatchesBuzz(t *testing.T) {
	k, err := KeysFromHex(buzzSecret)
	if err != nil {
		t.Fatal(err)
	}
	if k.PublicKey() != buzzPublic {
		t.Fatalf("public key = %s, want %s", k.PublicKey(), buzzPublic)
	}
	if k.SecretHex() != buzzSecret {
		t.Fatal("secret did not round-trip")
	}
}

func TestNPubVector(t *testing.T) {
	// From NIP-19.
	const hexKey = "3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"
	const npub = "npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6"
	got, err := EncodeNPub(hexKey)
	if err != nil || got != npub {
		t.Fatalf("EncodeNPub = %q, %v", got, err)
	}
	back, err := DecodeNPub(npub)
	if err != nil || back != hexKey {
		t.Fatalf("DecodeNPub = %q, %v", back, err)
	}
	if _, err := DecodeNPub(npub[:len(npub)-1] + "7"); err == nil {
		t.Fatal("a bad checksum decoded")
	}
}

func TestNormalizePubKey(t *testing.T) {
	k, _ := KeysFromHex(buzzSecret)
	for _, in := range []string{buzzPublic, strings.ToUpper(buzzPublic), " " + k.NPub() + " ", "nostr:" + k.NPub()} {
		got, err := NormalizePubKey(in)
		if err != nil || got != buzzPublic {
			t.Errorf("NormalizePubKey(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", strings.Repeat("0", 64), "npub1qqqq"} {
		if _, err := NormalizePubKey(in); err == nil {
			t.Errorf("NormalizePubKey(%q) accepted", in)
		}
	}
}

func TestSerializeEscaping(t *testing.T) {
	ev := &Event{
		PubKey:    buzzPublic,
		CreatedAt: 1700000000,
		Kind:      9,
		Tags:      Tags{{"h", "c1"}, {"p", "x", "", "mention"}},
		Content:   "a \"q\" \\ \n\t\r\b\f \x01 <&>   é",
	}
	want := `[0,"` + buzzPublic + `",1700000000,9,[["h","c1"],["p","x","","mention"]],"a \"q\" \\ \n\t\r\b\f \u0001 <&> ` + " " + ` é"]`
	if got := string(ev.Serialize()); got != want {
		t.Fatalf("Serialize =\n%s\nwant\n%s", got, want)
	}
	// The canonical form must be valid JSON that reads back to the same content.
	var arr []any
	if err := json.Unmarshal(ev.Serialize(), &arr); err != nil {
		t.Fatal(err)
	}
	if arr[5] != ev.Content {
		t.Fatal("content did not round-trip")
	}
}

func TestSignVerify(t *testing.T) {
	k, _ := GenerateKeys()
	ev := &Event{Kind: 9, Tags: Tags{{"h", "abc"}}, Content: "hello"}
	if err := ev.Sign(k); err != nil {
		t.Fatal(err)
	}
	if ev.PubKey != k.PublicKey() || len(ev.Sig) != 128 || ev.CreatedAt == 0 {
		t.Fatalf("signed event incomplete: %+v", ev)
	}
	if err := ev.Verify(); err != nil {
		t.Fatal(err)
	}
	tampered := *ev
	tampered.Content = "goodbye"
	if tampered.Verify() == nil {
		t.Fatal("tampered content verified")
	}
	tampered = *ev
	// An id that differs from the real one in its last digit. (Overwriting the last two digits
	// with "00" left the id unchanged whenever it already ended that way, 1 run in 256.)
	if len(ev.ID) != 64 {
		t.Fatalf("id %q is not 64 hex digits", ev.ID)
	}
	last := "0"
	if strings.HasSuffix(ev.ID, "0") {
		last = "1"
	}
	tampered.ID = ev.ID[:63] + last
	if tampered.Verify() == nil {
		t.Fatal("wrong id verified")
	}
	other, _ := GenerateKeys()
	tampered = *ev
	tampered.PubKey = other.PublicKey()
	tampered.ID = tampered.ComputeID()
	if tampered.Verify() == nil {
		t.Fatal("another key's claim verified")
	}

	// Signed correctly over an uppercased pubkey: a valid signature, but not the pubkey
	// string the allow and deny lists compare against, so it must not verify.
	upper := *ev
	upper.PubKey = strings.ToUpper(ev.PubKey)
	upper.ID = upper.ComputeID()
	sig, err := signDigest(k, upper.ID)
	if err != nil {
		t.Fatal(err)
	}
	upper.Sig = sig
	if upper.Verify() == nil {
		t.Fatal("uppercase pubkey verified")
	}
	upper = *ev
	upper.Sig = strings.ToUpper(ev.Sig)
	if upper.Verify() == nil {
		t.Fatal("uppercase signature verified")
	}
}

func TestTags(t *testing.T) {
	tags := Tags{{"h", "c"}, {"p", "a"}, {"p", "b", "wss://r"}, {"e", "x", "", "root"}, {"x"}}
	if tags.Value("h") != "c" || tags.Value("missing") != "" || tags.Value("x") != "" {
		t.Fatal("Value")
	}
	if got := tags.Values("p"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("Values = %v", got)
	}
	if !tags.Has("p", "b") || tags.Has("p", "c") {
		t.Fatal("Has")
	}
}

func TestHTTPAuthHeader(t *testing.T) {
	k, _ := GenerateKeys()
	h, err := HTTPAuthHeader(k, "post", "https://acme.example/query", []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(h, "Nostr "))
	if err != nil {
		t.Fatal(err)
	}
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	if err := ev.Verify(); err != nil {
		t.Fatal(err)
	}
	if ev.Kind != KindHTTPAuth || ev.Tags.Value("u") != "https://acme.example/query" || ev.Tags.Value("method") != "POST" {
		t.Fatalf("event = %+v", ev)
	}
	h2, _ := HTTPAuthHeader(k, "post", "https://acme.example/query", []byte(`{"a":1}`))
	if h2 == h {
		t.Fatal("two identical requests signed the same event (a replay guard would reject the second)")
	}
	// sha256 of {"a":1}
	if ev.Tags.Value("payload") != "015abd7f5cc57a2dd94b7590f04ad8084273905ee33ec5cebeae62276a97f862" {
		t.Fatalf("payload = %s", ev.Tags.Value("payload"))
	}
}

func TestAttestation(t *testing.T) {
	owner, _ := GenerateKeys()
	agent, _ := GenerateKeys()
	a, err := Attest(owner, agent.PublicKey(), "kind=9&created_at<4000000000")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseAttestation(a.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed.Verify(agent.PublicKey()); err != nil {
		t.Fatal(err)
	}
	other, _ := GenerateKeys()
	if parsed.Verify(other.PublicKey()) == nil {
		t.Fatal("verified for another agent")
	}
	if !parsed.ValidAt(time.Unix(3999999999, 0)) || parsed.ValidAt(time.Unix(4000000000, 0)) {
		t.Fatal("ValidAt")
	}
	kinds := &Attestation{Conditions: "kind=9&created_at<4000000000"}
	if !kinds.Allows(9, time.Unix(1, 0)) || kinds.Allows(22242, time.Unix(1, 0)) || kinds.Allows(9, time.Unix(4000000000, 0)) {
		t.Fatal("Allows")
	}
	malformed := &Attestation{Conditions: "created_at<x"}
	if malformed.ValidAt(time.Unix(0, 0)) {
		t.Fatal("a malformed bound allowed an event")
	}
	self, _ := Attest(agent, agent.PublicKey(), "")
	if self.Verify(agent.PublicKey()) == nil {
		t.Fatal("self-attestation verified")
	}

	for _, bad := range []string{
		``, `{}`, `["auth"]`,
		`["auth","` + owner.PublicKey() + `","kind=09","` + a.Sig + `"]`,
		`["auth","` + owner.PublicKey() + `","kind=1 ","` + a.Sig + `"]`,
		`["auth","` + strings.ToUpper(owner.PublicKey()) + `","","` + a.Sig + `"]`,
		`["nope","` + owner.PublicKey() + `","","` + a.Sig + `"]`,
	} {
		if _, err := ParseAttestation(bad); err == nil {
			t.Errorf("ParseAttestation(%q) accepted", bad)
		}
	}
}

func TestAuthEvent(t *testing.T) {
	k, _ := GenerateKeys()
	owner, _ := GenerateKeys()
	att, _ := Attest(owner, k.PublicKey(), "")
	ev, err := AuthEvent(k, "wss://acme.example", "chal", att)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != KindClientAuth || ev.Tags.Value("relay") != "wss://acme.example" || ev.Tags.Value("challenge") != "chal" ||
		ev.Tags.Value("auth") != owner.PublicKey() {
		t.Fatalf("auth event = %+v", ev)
	}
	if err := ev.Verify(); err != nil {
		t.Fatal(err)
	}
}
