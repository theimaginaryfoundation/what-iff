package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHydrateUserMessageImages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := &ModelContext{}
	m.AppendUserMessage(RoleUser, "hello", []UserMessageImage{{FileID: "file-abc", MediaType: "image/png"}}, false)

	calls := 0
	err := m.HydrateUserMessageImages(ctx, func(_ context.Context, id string) ([]byte, error) {
		calls++
		require.Equal(t, "file-abc", id)
		return []byte{1, 2, 3}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	last := m.Segments[len(m.Segments)-1]
	require.Len(t, last.UserImages, 1)
	require.Equal(t, []byte{1, 2, 3}, last.UserImages[0].RawBytes)
}

func TestHydrateUserMessageImages_NilFetchNoOp(t *testing.T) {
	t.Parallel()
	m := &ModelContext{}
	m.AppendUserMessage(RoleUser, "x", []UserMessageImage{{FileID: "f1"}}, false)
	require.NoError(t, m.HydrateUserMessageImages(context.Background(), nil))
	require.Empty(t, m.Segments[0].UserImages[0].RawBytes)
}

func TestHydrateUserMessageImages_FetchError(t *testing.T) {
	t.Parallel()
	m := &ModelContext{}
	m.AppendUserMessage(RoleUser, "x", []UserMessageImage{{FileID: "bad"}}, false)
	err := m.HydrateUserMessageImages(context.Background(), func(context.Context, string) ([]byte, error) {
		return nil, errors.New("boom")
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad")
}
