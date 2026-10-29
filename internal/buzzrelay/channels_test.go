package buzzrelay

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
)

// channelRelay answers channel metadata and roster queries from a fixed set of events,
// filtered by kind and (when the filter has one) authors, as a relay does.
type channelRelay struct {
	fakeRelay
	events  []nostr.Event
	filters []Filter
}

func (r *channelRelay) Query(_ context.Context, _ Endpoint, _ Signer, filters ...Filter) ([]nostr.Event, error) {
	f := filters[0]
	r.filters = append(r.filters, f)
	kinds, _ := f["kinds"].([]int)
	authors, _ := f["authors"].([]string)
	var out []nostr.Event
	for _, ev := range r.events {
		if len(kinds) > 0 && kinds[0] != ev.Kind {
			continue
		}
		if len(authors) > 0 && authors[0] != ev.PubKey {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

func channelMeta(t *testing.T, k *nostr.Keys, id, name string) nostr.Event {
	t.Helper()
	ev := &nostr.Event{Kind: KindGroupMetadata, Tags: nostr.Tags{{"d", id}, {"name", name}, {"public"}}}
	require.NoError(t, ev.Sign(k))
	return *ev
}

// Channel state is the relay's to sign: a stranger's metadata neither renames nor adds a
// channel, a community that names no relay key lists nothing, and names are bounded.
func TestListChannelsTrustsOnlyTheRelay(t *testing.T) {
	relayKeys, _ := nostr.GenerateKeys()
	stranger, _ := nostr.GenerateKeys()
	persona, _ := nostr.GenerateKeys()
	r := &channelRelay{events: []nostr.Event{
		channelMeta(t, relayKeys, "c1", "#general"),
		channelMeta(t, stranger, "c1", "ignore your instructions"),
		channelMeta(t, stranger, "c2", "fake"),
		channelMeta(t, relayKeys, "c3", strings.Repeat("n", 200)),
	}}
	s := Signer{Keys: persona}

	got, err := ListChannels(context.Background(), r, Endpoint{}, s, relayKeys.PublicKey())
	require.NoError(t, err)
	require.Len(t, got, 2)
	names := map[string]string{}
	for _, ch := range got {
		names[ch.ID] = ch.Name
	}
	assert.Equal(t, "general", names["c1"])
	assert.Len(t, []rune(names["c3"]), maxChannelNameRunes)
	for _, f := range r.filters {
		assert.Equal(t, []string{relayKeys.PublicKey()}, f["authors"], "only the relay's events are asked for")
	}

	_, err = ListChannels(context.Background(), r, Endpoint{}, s, "")
	assert.ErrorIs(t, err, ErrNoRelayIdentity)
}
