package fileattachment

import (
	"net/http"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutesExposesAttachmentContentRead(t *testing.T) {
	router := mux.NewRouter()
	h := &Handler{}
	h.RegisterRoutes(router)

	req, err := http.NewRequest(http.MethodGet, "/file-attachment/00000000-0000-0000-0000-000000000001/content", nil)
	require.NoError(t, err)

	match := &mux.RouteMatch{}
	require.True(t, router.Match(req, match), "uploaded text files need an authenticated content read route")
	require.NotNil(t, match.Handler, "content route must have a handler")
}
