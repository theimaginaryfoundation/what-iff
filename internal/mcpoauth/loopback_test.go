package mcpoauth

import (
	"net/http"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/netguard"
)

// loopbackHTTPClient reaches httptest servers, which listen on loopback and so are refused by
// the package's public-only default client.
func loopbackHTTPClient() *http.Client {
	return netguard.NewHTTPClient(netguard.PublicOrLoopbackControl, 15*time.Second)
}
