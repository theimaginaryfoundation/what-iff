package schema

import "entgo.io/ent"

// userPrivateFields and userPrivateEdges are splice points for deployed-only
// schema that does not ship in the open-source build. User.Fields and
// User.Edges append them, so the private overlay injects its additions by
// swapping this file (see the overlay's ent/schema/user_ext.go) without
// touching the public schema. The open-source build uses these no-op stubs.
func userPrivateFields() []ent.Field { return nil }

func userPrivateEdges() []ent.Edge { return nil }
