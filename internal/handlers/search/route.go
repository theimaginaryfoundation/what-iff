package search

import "github.com/google/uuid"

// SPA route segments. These mirror the Angular routes registered in
// `web/app/src/app/app.routes.ts` (singular nouns) so the client
// can navigate verbatim without a server-route -> client-route mapping layer.
//
// Images currently lack detail routes (the gallery is a single page). Phase 8
// (`/image-gallery/:id`) will introduce them; until then we route to the
// listing page.
const (
	routeChat        = "/chat"
	routePersonality = "/personality/"
	routeRitual      = "/ritual/"
	routeMemory      = "/memory/"
	routeGallery     = "/image-gallery"
)

// routeFor returns the SPA route the client should navigate to when the user
// activates the given result. Returns the empty string for unknown types so
// the handler can drop malformed entries instead of producing dead links.
func routeFor(resourceType string, id uuid.UUID) string {
	switch resourceType {
	case TypeChat:
		return routeChat + "/" + id.String()
	case TypePersonality:
		return routePersonality + id.String()
	case TypeRitual:
		return routeRitual + id.String()
	case TypeMemory:
		return routeMemory + id.String()
	case TypeImage:
		return routeGallery
	default:
		return ""
	}
}
