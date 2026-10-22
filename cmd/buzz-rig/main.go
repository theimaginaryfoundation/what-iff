// Command buzz-rig drives a local Buzz relay as a person would, for trying the Buzz relay
// (docs/adr/0x028-buzz-relay.md) without the Buzz desktop app. It is a development tool:
// every key it takes is a test key passed on the command line or in the environment.
//
//	buzz-rig attest   -owner <hex secret> -agent <npub|hex>                 print an owner attestation for a persona
//	buzz-rig channel  -relay <url> -owner <hex secret> -name <name> [-add <npub|hex>]...  create a channel, add members
//	buzz-rig invite   -relay <url> -owner <hex secret>                       mint an invite link (owner/admin only)
//	buzz-rig say      -relay <url> -as <hex secret> -channel <id> -text <text> [-mention <npub|hex>]... [-reply <event id>] [-file <path>]
//	buzz-rig watch    -relay <url> -as <hex secret> -channel <id> [-for 60s]  print the channel's messages as they arrive
//	buzz-rig channels -relay <url> -as <hex secret>                          list channels
//
// -relay accepts http:// and ws:// (local relays). BUZZ_RIG_OWNER / BUZZ_RIG_AS supply -owner / -as.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	relay := fs.String("relay", "http://localhost:3000", "community address")
	owner := fs.String("owner", os.Getenv("BUZZ_RIG_OWNER"), "owner/admin secret key (hex)")
	as := fs.String("as", os.Getenv("BUZZ_RIG_AS"), "secret key (hex) to act as")
	agent := fs.String("agent", "", "persona public key (npub or hex)")
	name := fs.String("name", "", "channel name")
	channel := fs.String("channel", "", "channel id")
	text := fs.String("text", "", "message text")
	reply := fs.String("reply", "", "event id to reply to (in its thread)")
	file := fs.String("file", "", "a file to attach (uploaded to the community's media store)")
	conditions := fs.String("conditions", "", "NIP-OA conditions (e.g. created_at<1900000000)")
	watchFor := fs.Duration("for", time.Minute, "how long to watch")
	var add, mention multi
	fs.Var(&add, "add", "member to add (repeatable)")
	fs.Var(&mention, "mention", "pubkey to mention (repeatable)")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), *watchFor+30*time.Second)
	defer cancel()
	c := buzzrelay.NewClient(true)
	ep, _, err := buzzrelay.ParseEndpoint(*relay, true)
	check(err)

	switch cmd {
	case "attest":
		k := keys(*owner, "-owner")
		pub, err := nostr.NormalizePubKey(*agent)
		check(err)
		att, err := nostr.Attest(k, pub, *conditions)
		check(err)
		fmt.Println(att.JSON())
	case "channel":
		s := buzzrelay.Signer{Keys: keys(*owner, "-owner")}
		if *name == "" {
			fail("-name is required")
		}
		id := uuid.NewString()
		publish(ctx, c, ep, s, buzzrelay.KindCreateGroup, nostr.Tags{{"h", id}, {"name", *name}, {"visibility", "open"}})
		for _, m := range add {
			pub, err := nostr.NormalizePubKey(m)
			check(err)
			publish(ctx, c, ep, s, buzzrelay.KindPutUser, nostr.Tags{{"h", id}, {"p", pub}})
		}
		fmt.Println(id)
	case "invite":
		u, err := c.MintInvite(ctx, ep, buzzrelay.Signer{Keys: keys(*owner, "-owner")}, 72*time.Hour, 0)
		check(err)
		fmt.Println(u)
	case "say":
		k := keys(*as, "-as")
		s := buzzrelay.Signer{Keys: k}
		var mentions []string
		for _, m := range mention {
			pub, err := nostr.NormalizePubKey(m)
			check(err)
			mentions = append(mentions, pub)
		}
		var thread *buzzrelay.ThreadRef
		if *reply != "" {
			events, err := c.Query(ctx, ep, s, buzzrelay.Filter{"ids": []string{*reply}, "limit": 1})
			check(err)
			if len(events) == 0 {
				fail("no such event: " + *reply)
			}
			t := buzzrelay.ReplyThread(events[0])
			thread = &t
		}
		var media [][]string
		if *file != "" {
			data, err := os.ReadFile(*file)
			check(err)
			blob, err := c.Upload(ctx, ep, s, filepath.Base(*file), http.DetectContentType(data), data)
			check(err)
			media = append(media, blob.Imeta(filepath.Base(*file)))
		}
		ev, err := buzzrelay.MessageEvent(s, *channel, *text, thread, mentions, media)
		check(err)
		check(c.Publish(ctx, ep, s, ev))
		fmt.Println(ev.ID)
	case "watch":
		s := buzzrelay.Signer{Keys: keys(*as, "-as")}
		names := map[string]string{}
		session := c.Open(ctx, buzzrelay.SessionConfig{
			Endpoint: ep, Signer: s, Start: time.Now().Add(-10 * time.Minute),
			Subs: map[string]buzzrelay.Filter{"c": {"kinds": []int{buzzrelay.KindStreamMessage}, "#h": []string{*channel}}},
			OnEvent: func(_ string, ev nostr.Event) {
				who, ok := names[ev.PubKey]
				if !ok {
					who = buzzrelay.ShortNPub(ev.PubKey)
					if p, err := c.Query(ctx, ep, s, buzzrelay.Filter{"kinds": []int{0}, "authors": []string{ev.PubKey}, "limit": 1}); err == nil && len(p) > 0 {
						if l := buzzrelay.ParseProfile(p[0]).Label(); l != "" {
							who = l
						}
					}
					names[ev.PubKey] = who
				}
				t := buzzrelay.ThreadOf(ev)
				where := ""
				if t.Root != "" {
					where = " (in thread " + t.Root[:8] + ")"
				}
				fmt.Printf("%s %s%s [%s]: %s\n", time.Unix(ev.CreatedAt, 0).Format("15:04:05"), who, where, ev.ID[:8], ev.Content)
			},
			OnState: func(st buzzrelay.SessionState, detail string) { fmt.Fprintf(os.Stderr, "[%s] %s\n", st, detail) },
		})
		time.Sleep(*watchFor)
		_ = session.Close()
	case "channels":
		s := buzzrelay.Signer{Keys: keys(*as, "-as")}
		info, err := c.Info(ctx, ep)
		check(err)
		chans, err := buzzrelay.ListChannels(ctx, c, ep, s, info.Self)
		check(err)
		out, _ := json.MarshalIndent(chans, "", "  ")
		fmt.Println(string(out))
	default:
		usage()
	}
}

func publish(ctx context.Context, c *buzzrelay.Client, ep buzzrelay.Endpoint, s buzzrelay.Signer, kind int, tags nostr.Tags) {
	ev := &nostr.Event{Kind: kind, Tags: tags}
	check(ev.Sign(s.Keys))
	check(c.Publish(ctx, ep, s, ev))
}

func keys(secret, flagName string) *nostr.Keys {
	if secret == "" {
		fail(flagName + " is required")
	}
	k, err := nostr.KeysFromHex(secret)
	check(err)
	return k
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "buzz-rig:", msg)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: buzz-rig attest|channel|say|watch|channels [flags] (see the package comment)")
	os.Exit(2)
}
