package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type mockUserStore struct {
	updatePasswordErr error
	createUserCalled  bool
	createUserReq     models.UserRegisterRequest
}

func (m *mockUserStore) CreateUser(_ context.Context, req models.UserRegisterRequest) (*models.UserResponse, *models.TokenPair, error) {
	m.createUserCalled = true
	m.createUserReq = req
	return &models.UserResponse{
		ID:       uuid.New(),
		Username: req.Username,
		Email:    req.Email,
		Role:     "user",
		Status:   "active",
	}, &models.TokenPair{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
	}, nil
}

func (m *mockUserStore) GetUserByCredentials(context.Context, models.UserLoginRequest) (*models.UserResponse, *models.TokenPair, error) {
	panic("not implemented")
}

func (m *mockUserStore) GetUserByID(context.Context, uuid.UUID) (*models.UserResponse, error) {
	panic("not implemented")
}

func (m *mockUserStore) UpdateUserProfile(context.Context, uuid.UUID, models.UpdateUserRequest) (*models.UserResponse, error) {
	panic("not implemented")
}

func (m *mockUserStore) UpdateUserPassword(context.Context, uuid.UUID, models.UpdatePasswordRequest) error {
	return m.updatePasswordErr
}

func (m *mockUserStore) DeleteUser(context.Context, uuid.UUID) error {
	panic("not implemented")
}

func (m *mockUserStore) RefreshUserToken(context.Context, string) (*models.TokenPair, error) {
	panic("not implemented")
}

func (m *mockUserStore) GetUserPreferences(context.Context, uuid.UUID) (*models.UserPreferences, error) {
	return nil, nil
}

func (m *mockUserStore) UpdateUserPreferences(context.Context, uuid.UUID, models.UserPreferences) (*models.UserPreferences, error) {
	return nil, nil
}

func (m *mockUserStore) GetUsageStats(context.Context, time.Time, time.Time, uuid.UUID) (*models.UsageStats, error) {
	return nil, nil
}

func TestUpdatePasswordReturnsBadRequestForExternalAuthUsers(t *testing.T) {
	handler := &Handler{
		store:         &mockUserStore{updatePasswordErr: datastore.ErrExternalPasswordUnsupported},
		logger:        zap.NewNop(),
		allowedEmails: nil,
	}

	body := `{"current_password":"old-pass","new_password":"newPass123"}`
	req := httptest.NewRequest(http.MethodPut, "/user/password", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, uuid.New())
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()

	handler.UpdatePassword(rr, req)

	res := rr.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.StatusCode)
	}

	var errResp models.ErrorResponse
	if err := json.NewDecoder(res.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if errResp.Message != "Password is managed by your identity provider" {
		t.Fatalf("unexpected error message: %q", errResp.Message)
	}
}

func TestRegisterBlocksPlusEmailAliasInProduction(t *testing.T) {
	store := &mockUserStore{}
	handler := NewHandler(store, zap.NewNop(), nil, "prod")

	body := `{"username":"testuser","email":"testuser+starter@example.com","password":"newPass123","terms_accepted":true}`
	req := httptest.NewRequest(http.MethodPost, "/user/register", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handler.Register(rr, req)

	res := rr.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.StatusCode)
	}
	if store.createUserCalled {
		t.Fatal("expected CreateUser not to be called")
	}

	var errResp models.ErrorResponse
	if err := json.NewDecoder(res.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if errResp.Message != "Email aliases with + are not allowed for signup" {
		t.Fatalf("unexpected error message: %q", errResp.Message)
	}
}

func TestRegisterAllowsPlusEmailAliasOutsideProduction(t *testing.T) {
	store := &mockUserStore{}
	handler := NewHandler(store, zap.NewNop(), nil, "dev")

	body := `{"username":"testuser","email":"testuser+dev@example.com","password":"newPass123","terms_accepted":true}`
	req := httptest.NewRequest(http.MethodPost, "/user/register", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handler.Register(rr, req)

	res := rr.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, res.StatusCode)
	}
	if !store.createUserCalled {
		t.Fatal("expected CreateUser to be called")
	}
	if store.createUserReq.Email != "testuser+dev@example.com" {
		t.Fatalf("unexpected email passed to CreateUser: %q", store.createUserReq.Email)
	}
}

func TestRegisterAllowsArbitraryEmailInDevelopmentWithNoAllowlist(t *testing.T) {
	store := &mockUserStore{}
	handler := NewHandler(store, zap.NewNop(), nil, "development")

	body := `{"username":"stranger","email":"stranger@example.com","password":"newPass123","terms_accepted":true}`
	req := httptest.NewRequest(http.MethodPost, "/user/register", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handler.Register(rr, req)

	res := rr.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, res.StatusCode)
	}
	if !store.createUserCalled {
		t.Fatal("expected CreateUser to be called")
	}
}

func TestRegisterAllowsEmailOnAllowlist(t *testing.T) {
	store := &mockUserStore{}
	handler := NewHandler(store, zap.NewNop(), []string{"stranger@example.com"}, "development")

	body := `{"username":"stranger","email":"stranger@example.com","password":"newPass123","terms_accepted":true}`
	req := httptest.NewRequest(http.MethodPost, "/user/register", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handler.Register(rr, req)

	res := rr.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, res.StatusCode)
	}
	if !store.createUserCalled {
		t.Fatal("expected CreateUser to be called")
	}
}

func TestRegisterBlocksEmailNotOnAllowlist(t *testing.T) {
	store := &mockUserStore{}
	handler := NewHandler(store, zap.NewNop(), []string{"allowed@example.com"}, "development")

	body := `{"username":"stranger","email":"stranger@example.com","password":"newPass123","terms_accepted":true}`
	req := httptest.NewRequest(http.MethodPost, "/user/register", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handler.Register(rr, req)

	res := rr.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, res.StatusCode)
	}
	if store.createUserCalled {
		t.Fatal("expected CreateUser not to be called")
	}

	var errResp models.ErrorResponse
	if err := json.NewDecoder(res.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if errResp.Message != "Registration is currently restricted. Contact support if you believe this is an error." {
		t.Fatalf("unexpected error message: %q", errResp.Message)
	}
}
