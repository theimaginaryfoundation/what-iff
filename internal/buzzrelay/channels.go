package buzzrelay

import (
	"context"
	"sort"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
)

// Channel is a community channel as the persona can see it.
type Channel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	About string `json:"about,omitempty"`
	// Public channels can be joined; private ones need a member to add the persona.
	Public bool `json:"public"`
	// Member is whether the persona is in the channel's roster.
	Member   bool `json:"member"`
	Archived bool `json:"archived,omitempty"`
}

// maxChannels bounds one listing (the relay caps a query at 1000).
const maxChannels = 500

// ListChannels lists the community's message channels the persona can see: those it is a
// member of and the public ones it could join. Channel state is relay-signed, so events not
// signed by the relay (relayPub, from NIP-11) are ignored when relayPub is known. Forum and
// archived channels are left out.
func ListChannels(ctx context.Context, r Relay, ep Endpoint, s Signer, relayPub string) ([]Channel, error) {
	me := s.Keys.PublicKey()
	meta, err := r.Query(ctx, ep, s, Filter{"kinds": []int{KindGroupMetadata}, "limit": maxChannels})
	if err != nil {
		return nil, err
	}
	rosters, err := r.Query(ctx, ep, s, Filter{"kinds": []int{KindGroupMembers}, "#p": []string{me}, "limit": maxChannels})
	if err != nil {
		return nil, err
	}
	trusted := func(ev nostr.Event) bool {
		return ev.Verify() == nil && (relayPub == "" || ev.PubKey == relayPub)
	}
	member := map[string]bool{}
	for _, ev := range rosters {
		if trusted(ev) && ev.Tags.Has("p", me) {
			member[ev.Tags.Value("d")] = true
		}
	}
	// Addressable events: keep the newest metadata per channel.
	latest := map[string]nostr.Event{}
	for _, ev := range meta {
		id := ev.Tags.Value("d")
		if id == "" || !trusted(ev) {
			continue
		}
		if cur, ok := latest[id]; !ok || ev.CreatedAt > cur.CreatedAt {
			latest[id] = ev
		}
	}
	out := make([]Channel, 0, len(latest))
	for id, ev := range latest {
		kind := ev.Tags.Value("t")
		if kind != "" && kind != "stream" {
			continue
		}
		ch := Channel{
			ID:       id,
			Name:     strings.TrimPrefix(ev.Tags.Value("name"), "#"),
			About:    ev.Tags.Value("about"),
			Public:   ev.Tags.Find("public") != nil || (ev.Tags.Find("private") == nil && ev.Tags.Find("closed") == nil),
			Member:   member[id],
			Archived: ev.Tags.Find("archived") != nil || ev.Tags.Value("archived") == "true",
		}
		if ch.Archived || (!ch.Public && !ch.Member) {
			continue
		}
		if ch.Name == "" {
			ch.Name = id[:min(8, len(id))]
		}
		out = append(out, ch)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Member != out[j].Member {
			return out[i].Member
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}
