// Package sharelink serves share links (ADR 0x051): the owner's API to share a file and manage
// its link, and the public API the share site reads a shared file or web app through.
package sharelink

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Store is what the handler reads and writes.
type Store interface {
	gateStore
	ShareLinkForFile(ctx context.Context, userID, fileID uuid.UUID) (*models.ShareLink, error)
	ShareScope(ctx context.Context, userID, fileID uuid.UUID) (models.ShareScope, error)
	CreateShareLink(ctx context.Context, userID, fileID uuid.UUID, withPasscode bool) (*models.ShareLink, bool, error)
	UpdateShareLink(ctx context.Context, userID, fileID uuid.UUID, requirePasscode *bool, regenerate bool) (*models.ShareLink, error)
	DeleteShareLink(ctx context.Context, userID, fileID uuid.UUID) error
	ListShareLinks(ctx context.Context, userID uuid.UUID) ([]models.ShareLink, error)
	SharedItemByKey(ctx context.Context, key string) (*models.SharedItem, error)
	SharePasscode(item *models.SharedItem) (string, error)
	SharedProjectFiles(ctx context.Context, ownerID uuid.UUID, root string) ([]models.FileAttachment, error)
	SharedProjectFile(ctx context.Context, ownerID uuid.UUID, root, folder, name string) (*models.FileAttachment, error)
}

// Config is how a deployment runs share links.
type Config struct {
	// Secret is the server's signing secret (JWT_SECRET); the keys for unlock cookies and stored
	// addresses are derived from it, each for its own use.
	Secret string
	// ClientIPHeader names a header the deployment's proxy sets to the visitor's address and a
	// visitor cannot set (CloudFront-Viewer-Address); empty to use the connection's (ADR 0x051 §4).
	ClientIPHeader string
	// ProxySecret, when set, is what the proxy sends in ProxySecretHeader: ClientIPHeader is trusted
	// only on a request that carries it, as a visitor who reaches the API another way could set the
	// address header to anything. A deployment whose API is reachable other than through the proxy
	// sets it (CLIENT_IP_PROXY_SECRET).
	ProxySecret string
}

// Handler serves share links.
type Handler struct {
	ds             Store
	files          func() storage.FileStore
	logger         *zap.Logger
	sessionKey     []byte
	addressSecret  []byte
	clientIPHeader string
	proxySecret    string
	now            func() time.Time
}

// forgeableHeaders are address headers a visitor can set themselves: trusted without a proxy secret,
// they let a visitor choose their address on every unlock.
var forgeableHeaders = map[string]bool{"x-forwarded-for": true, "x-real-ip": true, "forwarded": true, "true-client-ip": true, "x-client-ip": true}

// NewHandler makes the share link handler; files gives the store a file's bytes are read from.
func NewHandler(ds Store, files func() storage.FileStore, logger *zap.Logger, cfg Config) *Handler {
	if cfg.ClientIPHeader != "" && cfg.ProxySecret == "" && forgeableHeaders[strings.ToLower(cfg.ClientIPHeader)] {
		logger.Warn("CLIENT_IP_HEADER names a header visitors can set themselves, and CLIENT_IP_PROXY_SECRET is not set: share-link unlock limits can be dodged by setting it",
			zap.String("client_ip_header", cfg.ClientIPHeader))
	}
	return &Handler{
		ds:             ds,
		files:          files,
		logger:         logger,
		sessionKey:     derive(cfg.Secret, "what-iff share session v1"),
		addressSecret:  derive(cfg.Secret, "what-iff share address v1"),
		clientIPHeader: cfg.ClientIPHeader,
		proxySecret:    cfg.ProxySecret,
		now:            time.Now,
	}
}

func derive(secret, use string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(use))
	return mac.Sum(nil)
}

// RegisterRoutes adds the owner's routes (signed in).
func (h *Handler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/share-links", h.ListShareLinks).Methods("GET")
	r.HandleFunc("/share-links/files/{fileId}", h.GetShareLink).Methods("GET")
	r.HandleFunc("/share-links/files/{fileId}", h.PutShareLink).Methods("PUT")
	r.HandleFunc("/share-links/files/{fileId}", h.PatchShareLink).Methods("PATCH")
	r.HandleFunc("/share-links/files/{fileId}", h.DeleteShareLink).Methods("DELETE")
}

// keyPattern is a share key as the routes take it: 22 base64url characters.
const keyPattern = "{key:[A-Za-z0-9_-]{22}}"

// RegisterPublicRoutes adds the visitors' routes (no sign-in), which the share site reaches on its
// own origin (ADR 0x051 §5).
func (h *Handler) RegisterPublicRoutes(r *mux.Router) {
	r.HandleFunc("/shared/"+keyPattern, h.GetShared).Methods("GET")
	r.HandleFunc("/shared/"+keyPattern+"/unlock", h.Unlock).Methods("POST")
	r.HandleFunc("/shared/"+keyPattern+"/content", h.GetSharedContent).Methods("GET")
	r.HandleFunc("/shared/"+keyPattern+"/files", h.ListSharedFiles).Methods("GET")
	r.HandleFunc("/shared/"+keyPattern+"/files/{path:.+}", h.GetSharedFile).Methods("GET")
}
