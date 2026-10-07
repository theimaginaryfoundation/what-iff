package main

// First-party plugins shipped in this repository. Each package registers itself
// with internal/plugins (and, where it needs one, internal/replyhook) from its
// init(), and the server applies it during setup; linking the package is what
// turns it on. Other builds add their own plugins with blank imports in files of
// their own.
import (
	// The Discord relay: personas answer in Discord channels as their own bots.
	// Set DISCORD_RELAY_ENABLED=false to switch it off (docs/adr/0x025-discord-relay.md).
	_ "github.com/theimaginaryfoundation/what-iff/internal/discordplugin"
)
