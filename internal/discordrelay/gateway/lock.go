package gateway

import "github.com/theimaginaryfoundation/what-iff/internal/relaykit"

// DefaultLockKey is the advisory lock key for gateway leadership. The agent-job
// scheduler's default is 80920031; this is the next one, so the two never
// contend. Override with DISCORD_GATEWAY_LOCK_KEY. (The Buzz gateway uses 80920033.)
const DefaultLockKey int64 = 80920032

// AdvisoryLocker leads through the datastore's Postgres advisory lock
// (relaykit.AdvisoryLocker).
type AdvisoryLocker = relaykit.AdvisoryLocker

// SingleInstanceLocker always leads. For a single-process deployment, or a local
// database without advisory locks (sqlite); never with more than one API task.
type SingleInstanceLocker = relaykit.SingleInstanceLocker
