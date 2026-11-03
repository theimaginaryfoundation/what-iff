package nostr

import (
	"encoding/hex"
	"errors"
	"strings"
)

// NIP-19 bech32 encoding (BIP-173) for npub keys. Only the plain key forms are
// needed (no TLV nprofile/nevent).

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

var bech32Gen = [5]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}

func bech32Polymod(values []byte) uint32 {
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>uint(i))&1 == 1 {
				chk ^= bech32Gen[i]
			}
		}
	}
	return chk
}

func bech32HRPExpand(hrp string) []byte {
	out := make([]byte, 0, len(hrp)*2+1)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]>>5)
	}
	out = append(out, 0)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]&31)
	}
	return out
}

// convertBits regroups a byte slice from one bit width to another.
func convertBits(data []byte, from, to uint, pad bool) ([]byte, error) {
	acc, bits := uint32(0), uint(0)
	maxv := uint32(1)<<to - 1
	var out []byte
	for _, b := range data {
		if uint32(b)>>from != 0 {
			return nil, errors.New("bech32: invalid data")
		}
		acc = acc<<from | uint32(b)
		bits += from
		for bits >= to {
			bits -= to
			out = append(out, byte(acc>>bits&maxv))
		}
	}
	if pad {
		if bits > 0 {
			out = append(out, byte(acc<<(to-bits)&maxv))
		}
	} else if bits >= from || acc<<(to-bits)&maxv != 0 {
		return nil, errors.New("bech32: invalid padding")
	}
	return out, nil
}

func bech32Encode(hrp string, data []byte) (string, error) {
	values, err := convertBits(data, 8, 5, true)
	if err != nil {
		return "", err
	}
	exp := append(bech32HRPExpand(hrp), values...)
	mod := bech32Polymod(append(exp, 0, 0, 0, 0, 0, 0)) ^ 1
	var b strings.Builder
	b.WriteString(hrp)
	b.WriteByte('1')
	for _, v := range values {
		b.WriteByte(bech32Charset[v])
	}
	for i := 0; i < 6; i++ {
		b.WriteByte(bech32Charset[(mod>>uint(5*(5-i)))&31])
	}
	return b.String(), nil
}

func bech32Decode(s string) (string, []byte, error) {
	if strings.ToLower(s) != s && strings.ToUpper(s) != s {
		return "", nil, errors.New("bech32: mixed case")
	}
	s = strings.ToLower(s)
	pos := strings.LastIndexByte(s, '1')
	if pos < 1 || pos+7 > len(s) || len(s) > 90 {
		return "", nil, errors.New("bech32: invalid length or separator")
	}
	hrp := s[:pos]
	values := make([]byte, 0, len(s)-pos-1)
	for i := pos + 1; i < len(s); i++ {
		d := strings.IndexByte(bech32Charset, s[i])
		if d < 0 {
			return "", nil, errors.New("bech32: invalid character")
		}
		values = append(values, byte(d))
	}
	if bech32Polymod(append(bech32HRPExpand(hrp), values...)) != 1 {
		return "", nil, errors.New("bech32: bad checksum")
	}
	data, err := convertBits(values[:len(values)-6], 5, 8, false)
	if err != nil {
		return "", nil, err
	}
	return hrp, data, nil
}

// EncodeNPub encodes a hex public key as an npub.
func EncodeNPub(pubkeyHex string) (string, error) {
	raw, err := hex.DecodeString(pubkeyHex)
	if err != nil || len(raw) != 32 {
		return "", errors.New("nostr: public key must be 32 bytes of hex")
	}
	return bech32Encode("npub", raw)
}

// DecodeNPub decodes an npub to a hex public key.
func DecodeNPub(npub string) (string, error) {
	hrp, data, err := bech32Decode(strings.TrimSpace(npub))
	if err != nil {
		return "", err
	}
	if hrp != "npub" || len(data) != 32 {
		return "", errors.New("nostr: not an npub")
	}
	return hex.EncodeToString(data), nil
}
