package models

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// rawToken wraps arbitrary content as a valid base64url payload so the negative decode cases
// exercise the post-decode parsing (delimiter, time, uuid) rather than the base64 step.
func rawToken(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestMessageCursorRoundTrip(t *testing.T) {
	// Sub-second precision must survive the round trip so the keyset comparison stays exact.
	sentAt := time.Date(2026, 9, 21, 10, 30, 0, 123456789, time.UTC)
	id := uuid.New()

	token := EncodeMessageCursor(sentAt, id)
	require.NotContains(t, token, "+", "token must be URL-safe base64")
	require.NotContains(t, token, "/", "token must be URL-safe base64")
	require.NotContains(t, token, "=", "token must be unpadded")

	gotSentAt, gotID, err := DecodeMessageCursor(token)
	require.NoError(t, err)
	require.True(t, sentAt.Equal(gotSentAt), "sent_at round-trips exactly")
	require.Equal(t, id, gotID)
}

func TestDecodeMessageCursorRejectsGarbage(t *testing.T) {
	cases := []string{
		"not base64!!!",          // invalid base64
		rawToken("no-delimiter"), // decodes but has no "|"
		rawToken("2026-09-21T10:30:00Z|not-a-uuid"),
		rawToken("not-a-time|" + uuid.New().String()),
	}
	for _, c := range cases {
		_, _, err := DecodeMessageCursor(c)
		require.Error(t, err, "cursor %q should be rejected", c)
	}
}
