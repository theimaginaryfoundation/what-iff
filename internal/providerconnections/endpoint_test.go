package providerconnections

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestResolverEndpoint(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	presets := Presets(map[models.ModelProvider]string{models.ModelProviderOpenAI: "https://api.openai.example/v1"}, "localhost")
	r := NewResolver(f.store, presets)

	ollama, err := f.store.Create(ctx, f.alice, NewConnection{Preset: PresetOllama, Name: "Ollama"})
	require.NoError(t, err)
	custom, err := f.store.Create(ctx, f.alice, NewConnection{Preset: PresetCustom, Name: "Mine", Flavor: FlavorAnthropic, BaseURL: "http://localhost:9000", Key: aliceOpenAI})
	require.NoError(t, err)
	builtin, err := f.store.SetBuiltinKey(ctx, f.alice, models.ModelProviderOpenAI, aliceOpenAI)
	require.NoError(t, err)

	got, err := r.Endpoint(ctx, f.alice, ollama.ID)
	require.NoError(t, err)
	require.Equal(t, Endpoint{Preset: PresetOllama, Flavor: FlavorOpenAI, BaseURL: "http://localhost:11434/v1"}, got,
		"a connection without an address is reached at its preset's, with no key")

	got, err = r.Endpoint(ctx, f.alice, custom.ID)
	require.NoError(t, err)
	require.Equal(t, Endpoint{Preset: PresetCustom, Flavor: FlavorAnthropic, BaseURL: "http://localhost:9000", Key: aliceOpenAI}, got,
		"its own address, flavor and key, decrypted")

	_, err = r.Endpoint(ctx, f.bob, ollama.ID)
	require.ErrorIs(t, err, ErrNotFound, "another account's connection is not found")
	_, err = r.Endpoint(ctx, f.alice, uuid.New())
	require.ErrorIs(t, err, ErrNotFound)

	saved := CustomAddressesAllowed
	t.Cleanup(func() { CustomAddressesAllowed = saved })
	CustomAddressesAllowed = func() bool { return false }
	_, err = r.Endpoint(ctx, f.alice, ollama.ID)
	require.ErrorIs(t, err, ErrAddressNotAllowed, "connections already stored stop when custom addresses are turned off")
	_, err = r.Endpoint(ctx, f.alice, custom.ID)
	require.ErrorIs(t, err, ErrAddressNotAllowed)
	got, err = r.Endpoint(ctx, f.alice, builtin.ID)
	require.NoError(t, err, "a built-in provider's fixed address is still allowed")
	require.Equal(t, "https://api.openai.example/v1", got.BaseURL)
}
