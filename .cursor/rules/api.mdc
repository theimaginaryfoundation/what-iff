---
description: 
globs: internal/auth/*.go,internal/handlers/**/*.go,internal/server/*.go
alwaysApply: false
---
# API Server Design and Structure

## Overview

The API server is a RESTful service built with Go, using the Gorilla Mux router for routing and Ent for database operations. The server follows a modular design with clear separation of concerns and dependency injection patterns.

## Server Initialization

The API server is initialized in `cmd/api-server/main.go`. It follows these steps:

1. Load environment variables
2. Initialize logger
3. Connect to database
4. Create and configure server
5. Start server and handle graceful shutdown

```go
func main() {
    // Load environment variables
    if err := godotenv.Load(envFile); err != nil {
        log.Printf("Warning: Error loading .env file: %v", err)
    }

    // Initialize logger
    logger, err := zap.NewProduction()
    if err != nil {
        log.Fatalf("Failed to initialize logger: %v", err)
    }
    defer logger.Sync()

    // Initialize database connection
    client, err := database.NewClient(logger)
    if err != nil {
        logger.Fatal("Failed to connect to database", zap.Error(err))
    }
    defer client.Close()

    // Create server config
    config := server.NewConfig()

    // Create and configure server
    srv := server.NewServer(config, logger, client)

    // Run server in a goroutine
    go func() {
        if err := srv.Start(); err != nil {
            logger.Fatal("Server failed", zap.Error(err))
        }
    }()

    // Handle graceful shutdown
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    sig := <-c
    
    ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
    defer cancel()
    
    if err := srv.Shutdown(ctx); err != nil {
        logger.Fatal("Server forced to shutdown", zap.Error(err))
    }
}
```

## Server Configuration

Server configuration is managed through the `Config` struct in `internal/server/config.go`. It loads settings from environment variables with sensible defaults.

```go
type Config struct {
    Host          string
    Port          string
    ReadTimeout   time.Duration
    WriteTimeout  time.Duration
    IdleTimeout   time.Duration
    RunMigrations bool
}

func NewConfig() *Config {
    port := os.Getenv("SERVER_PORT")
    if port == "" {
        port = "8080"
    }

    host := os.Getenv("SERVER_HOST")
    if host == "" {
        host = "localhost"
    }

    return &Config{
        Host:          host,
        Port:          port,
        ReadTimeout:   15 * time.Second,
        WriteTimeout:  15 * time.Second,
        IdleTimeout:   60 * time.Second,
        RunMigrations: false,
    }
}
```

## Server Structure

The server is structured around a central `Server` struct in `internal/server/server.go` which encapsulates:

- Configuration
- Logger
- Router
- HTTP server
- Database client

```go
type Server struct {
    config *Config
    logger *zap.Logger
    router *mux.Router
    server *http.Server
    db     *ent.Client
}

func NewServer(config *Config, logger *zap.Logger, db *ent.Client) *Server {
    s := &Server{
        config: config,
        logger: logger,
        router: mux.NewRouter(),
        db:     db,
    }

    s.setupMiddleware()
    s.setupRoutes()

    s.server = &http.Server{
        Addr:         fmt.Sprintf("%s:%s", config.Host, config.Port),
        Handler:      s.router,
        WriteTimeout: config.WriteTimeout,
        ReadTimeout:  config.ReadTimeout,
        IdleTimeout:  config.IdleTimeout,
    }

    return s
}
```

## Provider Pattern

We use the provider pattern to decouple handlers from data access implementations. Our approach has evolved to use composite provider interfaces for handlers that need access to multiple entity types.

### Entity-Specific Provider Interfaces

Each entity has its own provider interface in the `internal/providers/` directory:

```go
// ContentBriefProvider defines the interface for content brief data operations
type ContentBriefProvider interface {
    CreateContentBrief(ctx context.Context, userID uuid.UUID, contentBrief models.ContentBrief) (*models.ContentBrief, error)
    ListContentBriefs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.ContentBriefFilters) (*models.PaginatedResponse, error)
    GetContentBrief(ctx context.Context, userID, id uuid.UUID) (*models.ContentBrief, error)
    UpdateContentBrief(ctx context.Context, userID uuid.UUID, contentBrief models.ContentBrief) (*models.ContentBrief, error)
    DeleteContentBrief(ctx context.Context, userID, id uuid.UUID) error
}
```

### Composite Provider Interfaces

For handlers that need access to multiple entity types, we define composite interfaces that extend multiple entity provider interfaces:

```go
// ProjectBriefQuestionIdeaJobProvider combines multiple provider interfaces
type ProjectBriefQuestionIdeaJobProvider interface {
    providers.ProjectProvider
    providers.ContentIdeaProvider
    providers.ContentBriefProvider
    providers.InterviewQuestionProvider
    providers.JobProvider
}
```

This approach simplifies handler constructors and avoids large parameter lists.

### Generator Interfaces

For AI-powered content generation, we define handler-specific generator interfaces that include only the methods needed by each handler:

```go
// ContentBriefQuestionGenerator defines the interface for generating content briefs and questions
type ContentBriefQuestionGenerator interface {
    GenerateContentBrief(ctx context.Context, contentIdea *models.ContentIdea, project *models.Project) (*models.CreateContentBrief, error)
    GenerateInterviewQuestions(ctx context.Context, brief *models.ContentBrief) ([]*models.InterviewQuestion, string, error)
}
```

This approach keeps the interfaces focused and ensures that handlers only depend on the generation capabilities they actually need.

### Datastore Implementation

The datastore implements all provider interfaces:

```go
// Datastore implements various provider interfaces
type Datastore struct {
    dbClient *ent.Client
    logger   *zap.Logger
}

func NewDatastore(dbClient *ent.Client, logger *zap.Logger) *Datastore {
    return &Datastore{
        dbClient: dbClient,
        logger:   logger,
    }
}
```

## Handler Organization

Handlers are organized by domain/resource in the `internal/handlers/` directory. Each handler package follows a consistent pattern:

1. Define provider and generator interfaces in `provider.go`
2. A handler struct that depends on these interfaces in `handler.go`
3. A constructor function that injects the dependencies
4. A method to register routes with a router
5. Handler methods for various operations in separate files (e.g., `create.go`, `list.go`, etc.)

Example handler structure:

```go
// Handler struct holds dependencies via interfaces
type Handler struct {
    dsProvider ProjectBriefQuestionDraftJobProvider
    generator  ContentDraftGenerator
    logger     *zap.Logger
}

// Constructor function with dependency injection
func NewHandler(dsProvider ProjectBriefQuestionDraftJobProvider, generator ContentDraftGenerator, logger *zap.Logger) *Handler {
    return &Handler{
        dsProvider: dsProvider,
        generator:  generator,
        logger:     logger,
    }
}

// Route registration method
func (h *Handler) RegisterRoutes(router *mux.Router) {
    resourceRouter := router.PathPrefix("/content-draft").Subrouter()
    
    resourceRouter.HandleFunc("", h.ListContentDrafts).Methods("GET")
    resourceRouter.HandleFunc("", h.CreateContentDraft).Methods("POST")
    resourceRouter.HandleFunc("/generate", h.GenerateContentDraft).Methods("POST")
    resourceRouter.HandleFunc("/{id}", h.GetContentDraft).Methods("GET")
    resourceRouter.HandleFunc("/{id}", h.UpdateContentDraft).Methods("PUT")
    resourceRouter.HandleFunc("/{id}", h.DeleteContentDraft).Methods("DELETE")
}
```

## Path Parameter Handling

Gorilla Mux path variables should be accessed using `mux.Vars(r)`:

```go
// Get resource ID from URL
vars := mux.Vars(r)
idStr, ok := vars["id"]
if !ok || idStr == "" {
    h.respondWithError(w, http.StatusBadRequest, "Resource ID is required", nil)
    return
}

// Parse the ID
resourceID, err := uuid.Parse(idStr)
if err != nil {
    h.respondWithError(w, http.StatusBadRequest, "Invalid resource ID", err)
    return
}
```

## Route Registration

Routes are registered in the `setupRoutes` method of the `Server` struct. The process follows these steps:

1. Initialize data providers
2. Initialize AI providers/generators
3. Initialize handlers with providers and generators
4. Create API router
5. Set up public routes
6. Set up protected routes with auth middleware

```go
func (s *Server) setupRoutes() {
    // Create data providers
    dataStore := datastore.NewDatastore(s.db, s.logger)
    
    // Create AI providers
    aiProvider := ai.NewOpenAIProvider(s.logger)

    // Create handlers
    userHandler := user.NewUserHandler(s.db, s.logger)
    contentBriefHandler := contentbrief.NewHandler(dataStore, aiProvider, s.logger)
    contentDraftHandler := contentdraft.NewHandler(dataStore, aiProvider, s.logger)

    // Setup API routes
    apiRouter := s.router.PathPrefix("/api").Subrouter()

    // Health check
    apiRouter.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("API is running"))
    }).Methods("GET")

    // Public routes
    apiRouter.HandleFunc("/users/register", userHandler.Register).Methods("POST")
    apiRouter.HandleFunc("/users/login", userHandler.Login).Methods("POST")

    // Protected routes
    authRouter := apiRouter.NewRoute().Subrouter()
    authRouter.Use(middleware.AuthMiddleware(s.db, s.logger))

    // Register protected routes
    userHandler.RegisterRoutes(authRouter)
    contentBriefHandler.RegisterRoutes(authRouter)
    contentDraftHandler.RegisterRoutes(authRouter)
}
```

## Authentication

Authentication is handled using JWT tokens. The `internal/auth` package provides functions for:

1. Generating access and refresh tokens
2. Validating tokens
3. Refreshing tokens
4. Password hashing and verification

```go
// Generate token pair
tokenPair, tokenID, err := auth.GenerateTokenPair(user.ID, user.Role)

// Validate access token
claims, err := auth.ValidateAccessToken(tokenString)

// Refresh access token
newAccessToken, err := auth.RefreshAccessToken(refreshToken)

// Hash password
hashedPassword, err := auth.HashPassword(password)

// Check password
isValid := auth.CheckPassword(password, hashedPassword)
```

## Authentication Middleware

The authentication middleware extracts and validates the JWT token from the Authorization header and adds the user ID to the request context.

```go
func AuthMiddleware(client *ent.Client, logger *zap.Logger) mux.MiddlewareFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            authHeader := r.Header.Get("Authorization")
            if authHeader == "" {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }

            // Extract the token
            tokenString := strings.Replace(authHeader, "Bearer ", "", 1)
            
            // Validate the token
            claims, err := auth.ValidateAccessToken(tokenString)
            if err != nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }

            // Add user ID to context
            ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
            
            // Call the next handler with the updated context
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

## Handler Utilities

The `internal/handlers/handlerutils` package provides common utilities for handlers:

### HTTP Response Utilities

```go
// Respond with JSON
func RespondWithJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
    response, err := json.Marshal(payload)
    if err != nil {
        log.Printf("Error marshaling JSON response: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    w.Write(response)
}

// Respond with error
func RespondWithError(w http.ResponseWriter, statusCode int, message string, err error) {
    if err != nil {
        log.Printf("Error: %v", err)
    }

    response := map[string]string{"error": message}
    RespondWithJSON(w, statusCode, response)
}
```

### JSON Decoding

```go
func DecodeJSON(r *http.Request, v interface{}) error {
    body, err := io.ReadAll(r.Body)
    if err != nil {
        return err
    }
    defer r.Body.Close()

    return json.Unmarshal(body, v)
}
```

### Parameter Parsing

```go
func ParseIntParam(v string, d int) int {
    if strings.TrimSpace(v) == "" {
        return d
    }

    i, err := strconv.Atoi(v)
    if err != nil {
        return d
    }

    return i
}
```

## Testing Best Practices

### Mock Providers

For testing, we use composite mock providers that implement all required interfaces for a handler:

```go
// MockCompositeProvider combines all the required providers into a single provider
type MockCompositeProvider struct {
    *MockContentDraftProvider
    *MockProjectProvider
    *MockContentBriefProvider
    *MockInterviewQuestionProvider
    *MockJobProvider
}

// NewMockCompositeProvider creates a new composite provider with all mock providers
func NewMockCompositeProvider() *MockCompositeProvider {
    return &MockCompositeProvider{
        MockContentDraftProvider:      NewMockContentDraftProvider(),
        MockProjectProvider:           NewMockProjectProvider(),
        MockContentBriefProvider:      NewMockContentBriefProvider(),
        MockInterviewQuestionProvider: NewMockInterviewQuestionProvider(),
        MockJobProvider:               NewMockJobProvider(),
    }
}
```

### Minimal Mock Implementations

For methods that are not used by the handler being tested, implement no-op versions that return appropriate errors:

```go
// DeleteContentIdea implements the interface but isn't used by this handler
func (m *MockCompositeProvider) DeleteContentIdea(ctx context.Context, userID, id uuid.UUID) error {
    return fmt.Errorf("not implemented: DeleteContentIdea not used by contentdraft handler")
}
```

This approach:
1. Makes it clear which methods are actually used by the handler
2. Reduces test maintenance burden
3. Causes tests to fail with clear error messages if a handler is modified to use a previously unused method

### Mock Generators

Similar to providers, create mock generators that implement only the methods required by the handler:

```go
// MockDraftGenerator is a mock implementation of the ContentDraftGenerator interface
type MockDraftGenerator struct {
    // Mock behavior flags
    shouldFail    bool
    errorToReturn error
}

// GenerateContentDraft mocks generating a content draft
func (m *MockDraftGenerator) GenerateContentDraft(ctx context.Context, brief *models.ContentBrief, project *models.Project, interviewQuestions []*models.InterviewQuestion, platformID *uuid.UUID) (*models.ContentDraft, error) {
    if m.shouldFail {
        return nil, m.errorToReturn
    }
    // Return mock implementation
    return &models.ContentDraft{
        // Set mock values
    }, nil
}
```

### Test Organization

Organize tests in separate files that match the handler files:

- `handler_test.go` - Mock implementations and helper functions
- `create_test.go` - Tests for the Create handler
- `list_test.go` - Tests for the List handler
- `get_test.go` - Tests for the Get handler
- `update_test.go` - Tests for the Update handler
- `delete_test.go` - Tests for the Delete handler
- `generate_test.go` - Tests for generation operations
- `error_test.go` - Tests for error scenarios

### Test Helper Functions

Create helper functions for common test operations:

```go
// Create a request with a user ID in the context
func createRequestWithUserID(method, url string, body interface{}) (*http.Request, error) {
    // Implementation
}

// Set resource ID in URL vars
func setResourceIDInURLVars(req *http.Request, resourceID uuid.UUID) *http.Request {
    vars := map[string]string{
        "id": resourceID.String(),
    }
    
    return mux.SetURLVars(req, vars)
}
```

## Best Practices

1. Use dependency injection and interfaces for loose coupling
2. Use composite interfaces for handlers that need multiple provider types
3. Define handler-specific generator interfaces with only required methods
4. Structure handlers in separate files for better organization
5. Use the provider pattern to decouple handlers from data access
6. Handle path variables correctly with Gorilla Mux
7. Create mock providers with no-op implementations for unused methods
8. Organize tests in separate files that match handler files
9. Always validate user input
10. Use appropriate HTTP status codes
11. Log errors with proper context
12. Follow consistent error handling patterns

## OpenAPI Specification Maintenance

An OpenAPI specification (openapi.yaml) is maintained at the project root to document the API. This specification is critical for:

1. Providing accurate documentation for API consumers
2. Enabling client code generation
3. Supporting API testing and validation
4. Ensuring consistency between documentation and implementation

### When to Update the OpenAPI Specification

The OpenAPI specification must be updated whenever:

1. A new API endpoint is added
2. An existing endpoint's behavior is changed
3. Request or response schemas are modified
4. Query parameters or path parameters are added, modified, or removed
5. Authentication requirements change
6. Error responses are modified

### How to Update the OpenAPI Specification

When making changes to the API:

1. First implement the API changes in code
2. Then update the corresponding sections in openapi.yaml:
   - Add new paths for new endpoints
   - Update operation methods as needed
   - Modify schema definitions for changed data models
   - Update parameter definitions
   - Ensure security requirements are correctly specified
   - Document all possible response codes

### Validation Best Practices

Before committing changes to the OpenAPI specification:

1. Validate the YAML syntax
2. Use an OpenAPI linter or validator tool
3. Ensure all required fields are documented
4. Check that schema references are valid
5. Verify that security schemes are correctly applied

### Example Update Process

When adding a new API endpoint:

```go
// 1. Add the new endpoint in the handler's RegisterRoutes method
func (h *Handler) RegisterRoutes(router *mux.Router) {
    // Existing routes
    resourceRouter.HandleFunc("", h.List).Methods("GET")
    resourceRouter.HandleFunc("", h.Create).Methods("POST")
    
    // New endpoint
    resourceRouter.HandleFunc("/export", h.ExportData).Methods("GET")
}

// 2. Then update the OpenAPI spec in openapi.yaml:
/*
  /resource/export:
    get:
      summary: Export resource data
      description: Export resources in various formats
      security:
        - bearerAuth: []
      parameters:
        - name: format
          in: query
          description: Export format (csv, json, xlsx)
          schema:
            type: string
            enum: [csv, json, xlsx]
            default: csv
      responses:
        '200':
          description: Exported data
          content:
            application/octet-stream:
              schema:
                type: string
                format: binary
        '401':
          description: Unauthorized
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Error'
*/
```

Remember that the OpenAPI specification is a contract between the API provider and consumers. Keeping it accurate and up-to-date is essential for the successful use of the API.
