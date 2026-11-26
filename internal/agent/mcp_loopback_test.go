package agent

import (
	"net/http"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/netguard"
)

// loopbackMCPHTTPClient reaches httptest MCP servers, which listen on loopback and so are
// refused by the MCP client's public-only default.
func loopbackMCPHTTPClient() *http.Client {
	return netguard.NewHTTPClient(netguard.PublicOrLoopbackControl, 15*time.Second)
}
