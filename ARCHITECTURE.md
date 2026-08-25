# Technical Architecture

This document provides a comprehensive overview of the Personal Assistant's technical architecture, design patterns, and implementation details.

## 🏛️ System Architecture Overview

The Personal Assistant follows a **clean architecture** design with clear separation of concerns, implementing a **full-stack application** with:

- **Backend**: Go-based RESTful API server
- **Frontend**: Angular single-page application  
- **Database**: PostgreSQL with pgvector extension
- **AI Integration**: OpenAI GPT models with embeddings
- **Authentication**: JWT-based stateless authentication

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Angular SPA   │    │   Go API Server │    │   PostgreSQL    │
│   (Frontend)    │◄──►│   (Backend)     │◄──►│   + pgvector    │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │   OpenAI API    │
                    │   (GPT Models)  │
                    └─────────────────┘
```

## 🔧 Backend Architecture

### Clean Architecture Layers

The backend follows **Domain-Driven Design** principles with clear layer separation:

```
┌─────────────────────────────────────────┐
│              HTTP Layer                 │
│         (Handlers & Middleware)         │
├─────────────────────────────────────────┤
│             Service Layer               │
│      (Business Logic & Providers)      │
├─────────────────────────────────────────┤
│             Data Layer                  │
│         (Datastore & Ent ORM)          │
├─────────────────────────────────────────┤
│           External Services             │
│         (OpenAI, Database, etc.)       │
└─────────────────────────────────────────┘
```

#### Directory Structure
```
internal/
├── handlers/           # HTTP request handlers (Controller layer)
├── middleware/         # HTTP middleware (CORS, auth, logging)
├── datastore/          # Data access layer (Repository pattern)
├── models/             # Domain models and DTOs
├── auth/              # Authentication utilities
├── agent/             # AI agent business logic
├── providers/         # External service providers
└── server/            # Server configuration and setup
```

### Dependency Injection Pattern

The application uses **constructor-based dependency injection** to maintain loose coupling:

```go
// Handler with injected dependencies
type Handler struct {
    store     datastore.Datastore
    logger    *zap.Logger
    aiAgent   *agent.Agent
}

func NewHandler(store datastore.Datastore, logger *zap.Logger, aiAgent *agent.Agent) *Handler {
    return &Handler{
        store:   store,
        logger:  logger, 
        aiAgent: aiAgent,
    }
}
```

### Provider Pattern

**Provider interfaces** decouple handlers from concrete implementations:

```go
// Provider interface defines data operations
type ChatProvider interface {
    CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
    GetChat(ctx context.Context, userID, chatID uuid.UUID) (*models.Chat, error)
    ListChats(ctx context.Context, userID uuid.UUID, filters models.ChatFilters) (*models.PaginatedResponse, error)
    UpdateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
    DeleteChat(ctx context.Context, userID, chatID uuid.UUID) error
}

// Datastore implements all provider interfaces
type Datastore struct {
    dbClient *ent.Client
    logger   *zap.Logger
}
```

### Handler Pattern

**HTTP handlers** follow a consistent structure with clear responsibilities:

```go
func (h *Handler) CreateChat(w http.ResponseWriter, r *http.Request) {
    // 1. Extract user ID from context
    userID := getUserIDFromContext(r.Context())
    
    // 2. Decode and validate request
    var req models.CreateChatRequest
    if err := handlerutils.DecodeJSON(r, &req); err != nil {
        h.respondWithError(w, http.StatusBadRequest, "Invalid request", err)
        return
    }
    
    // 3. Call business logic
    chat, err := h.store.CreateChat(r.Context(), userID, models.Chat{
        Name: req.Name,
    })
    if err != nil {
        h.handleDatastoreError(w, err)
        return
    }
    
    // 4. Return response
    handlerutils.RespondWithJSON(w, http.StatusCreated, chat)
}
```

### Middleware Architecture

**Middleware chain** handles cross-cutting concerns:

```go
func (s *Server) setupMiddleware() {
    s.router.Use(middleware.CORS())           // Enable CORS
    s.router.Use(middleware.RequestLogger())  // Log all requests
    s.router.Use(middleware.Recover())        // Panic recovery
}

func (s *Server) setupProtectedRoutes() {
    authRouter := s.router.PathPrefix("/api").Subrouter()
    authRouter.Use(middleware.AuthMiddleware(s.db, s.logger))  // JWT validation
}
```

### Authentication & Authorization

**JWT-based stateless authentication** with role-based access control:

```go
// JWT Claims structure
type Claims struct {
    UserID uuid.UUID `json:"user_id"`
    Role   string    `json:"role"`
    jwt.RegisteredClaims
}

// Authentication middleware
func AuthMiddleware(client *ent.Client, logger *zap.Logger) mux.MiddlewareFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Extract and validate JWT token
            claims, err := auth.ValidateAccessToken(getTokenFromHeader(r))
            if err != nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            
            // Add user context
            ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

## 🎨 Frontend Architecture

### Angular Application Structure

The frontend follows **Angular best practices** with standalone components and feature-based organization:

```
src/app/
├── core/                    # Singleton services and shared infrastructure
│   ├── components/         # Modal-style cross-feature components
│   ├── guards/            # Route guards for authentication
│   ├── interceptors/      # HTTP interceptors
│   ├── models/           # TypeScript interfaces
│   └── services/         # Singleton services
├── layout/                 # App shell: sidebar, command palette, right panel
├── features/              # Feature modules
│   ├── auth/            # Authentication components
│   ├── chat/            # Chat interface
│   ├── memory/          # Memory management
│   ├── personality/     # Personality management
│   └── profile/         # User profile
└── shared/               # Shared utilities, primitives, pipes
```

All authenticated routes are nested under `AppLayoutComponent` (in `layout/`),
which renders the persistent sidebar (with app/config mode switch), router
outlet, right context-panel slot, and the global command palette (⌘K). The
former top `<app-navbar>` was retired in Phase 03; navigation lives in the
left sidebar.

### Component Architecture

**Standalone components** with signal-based state management:

```typescript
@Component({
  selector: 'app-chat-page',
  standalone: true,
  imports: [MessageListComponent, ChatComposerComponent],
  providers: [ChatSessionService],
  templateUrl: './chat-page.component.html',
  styleUrl: './chat-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class ChatPageComponent implements OnInit {
  readonly session = inject(ChatSessionService);
  readonly groups = computed(() => groupMessages(this.session.messages()));

  ngOnInit() {
    // Route params select the active conversation session.
  }
}
```

### Service Architecture

**Angular services** handle data management and API communication:

```typescript
@Injectable({
  providedIn: 'root'
})
export class ChatService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/chat`;
  
  // BehaviorSubjects for reactive state
  private _chats$ = new BehaviorSubject<Chat[]>([]);
  private _activeChat$ = new BehaviorSubject<Chat | null>(null);
  
  // Public observables
  chats$ = this._chats$.asObservable();
  activeChat$ = this._activeChat$.asObservable();
  
  listChats(): Observable<Chat[]> {
    return this.http.get<Chat[]>(this.apiUrl).pipe(
      tap(chats => this._chats$.next(chats))
    );
  }
  
  createChat(chat: CreateChatRequest): Observable<Chat> {
    return this.http.post<Chat>(this.apiUrl, chat).pipe(
      tap(newChat => {
        const currentChats = this._chats$.value;
        this._chats$.next([newChat, ...currentChats]);
      })
    );
  }
}
```

### State Management

**Reactive state management** using RxJS and Angular Signals:

```typescript
// Service-level state with BehaviorSubjects
export class MemoryService {
  // Internal state
  private _memories$ = new BehaviorSubject<Memory[]>([]);
  private _totalCount$ = new BehaviorSubject<number>(0);
  private _isLoading$ = new BehaviorSubject<boolean>(false);
  
  // Public reactive streams
  memories$ = this._memories$.asObservable();
  totalCount$ = this._totalCount$.asObservable();
  isLoading$ = this._isLoading$.asObservable();
}

// Component-level state with Signals
export class MemoryListComponent {
  // Reactive signals
  memories = signal<Memory[]>([]);
  currentPage = signal(1);
  totalCount = signal(0);
  
  // Computed signals
  hasMemories = computed(() => this.memories().length > 0);
  totalPages = computed(() => Math.ceil(this.totalCount() / this.pageSize()));
}
```

## 🗄️ Database Architecture

### Schema Design

**PostgreSQL schema** designed with Ent ORM following best practices:

```go
// User entity with UUID primary key
func (User) Fields() []ent.Field {
    return []ent.Field{
        field.UUID("id", uuid.UUID{}).
            Default(uuid.New).
            Immutable(),
        field.String("username").
            Unique().
            MinLen(3).
            MaxLen(50),
        field.String("email").
            Unique().
            MaxLen(255),
        field.String("password_hash").
            Sensitive().
            NotEmpty(),
    }
}

// Relationships using edges
func (User) Edges() []ent.Edge {
    return []ent.Edge{
        edge.To("chats", Chat.Type).
            Annotations(entsql.Annotation{
                OnDelete: entsql.Cascade,
            }),
        edge.To("memories", Memory.Type),
        edge.To("personalities", Personality.Type),
    }
}

// Database indexes for performance
func (User) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("username").Unique(),
        index.Fields("email").Unique(),
        index.Fields("created_at"),
    }
}
```

### Entity Relationship Diagram

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│    User     │    │    Chat     │    │ ChatMessage │
│             │◄──►│             │◄──►│             │
│ id (UUID)   │    │ id (UUID)   │    │ id (UUID)   │
│ username    │    │ name        │    │ message     │
│ email       │    │ user_id     │    │ origin      │
│ password    │    │ model_id    │    │ chat_id     │
└──────┬──────┘    │personality_id│   │ rituals[]   │
       │            └──────┬──────┘    └──────┬──────┘
       │                   │                  │
       │                   │                  │
       ▼                   ▼                  ▼
┌─────────────┐    ┌─────────────┐    ┌──────────────┐
│   Memory    │    │    Model    │    │FileAttachment│
│             │    │             │    │              │
│ id (UUID)   │    │ id (UUID)   │    │ id (UUID)    │
│ content     │    │ name        │    │ name         │
│ scope       │    │ display_name│    │ file_id      │
│ user_id     │    │ provider    │    │ file_type    │
│ chat_id     │    │ tools_enabled│   │ user_id      │
└──────┬──────┘    └─────────────┘    │ chat_msg_id  │
       │                               │ personality_id│
       ▼                               └──────────────┘
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│ Embedding   │    │ Personality │    │   Ritual    │
│             │    │             │    │             │
│ id (UUID)   │    │ id (UUID)   │    │ id (UUID)   │
│ embedding   │◄───│ name        │    │ name        │
│ memory_id   │    │ system_prompt│   │ description │
└─────────────┘    │ user_id     │    │ content     │
 pgvector(1536)    └─────────────┘    │ hotkeys     │
                                       │ user_id     │
                   ┌─────────────┐    │ personality_id│
                   │    Job      │    └─────────────┘
                   │             │
                   │ id (UUID)   │
                   │ type        │
                   │ status      │
                   │ input/output│
                   │ user_id     │
                   │ chat_id     │
                   └─────────────┘
```

### Vector Embeddings Architecture

**Semantic search** using pgvector extension:

```go
// Memory entity with vector embeddings
type Memory struct {
    ID        uuid.UUID `json:"id"`
    Content   string    `json:"content"`
    UserID    uuid.UUID `json:"user_id"`
    ChatID    *uuid.UUID `json:"chat_id,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}

// Embedding storage with pgvector
func (Embedding) Fields() []ent.Field {
    return []ent.Field{
        field.UUID("id", uuid.UUID{}).Default(uuid.New),
        field.Other("embedding", &pgvector.Vector{}).
            SchemaType(map[string]string{
                dialect.Postgres: "vector(1536)",
            }),
        field.UUID("memory_id", uuid.UUID{}),
    }
}

// Semantic similarity search
func (d *Datastore) GetRelatedMemories(ctx context.Context, userID, chatID uuid.UUID, embedding []float32, limit int) ([]*models.Memory, error) {
    vector := pgvector.NewVector(embedding)
    vectorStr := vector.String()
    
    // Use vector similarity search with pgvector
    memories, err := d.dbClient.Memory.Query().
        Where(
            entmemory.HasOwnerWith(user.ID(userID)),
            entmemory.Or(
                entmemory.ScopeEQ(models.MemoryScopeGlobal),
                entmemory.And(
                    entmemory.ScopeEQ(models.MemoryScopeChat),
                    entmemory.HasChatWith(entchat.ID(chatID)),
                ),
            ),
        ).
        Modify(func(s *sql.Selector) {
            s.Join(sql.Table("embeddings")).On(
                s.C("id"), sql.Table("embeddings").C("memory_id"),
            )
            s.Where(sql.ExprP(fmt.Sprintf("embedding <-> '%s' <= %f", vectorStr, MemoryRelevanceThreshold)))
            s.OrderExpr(sql.Expr(fmt.Sprintf("embedding <-> '%s'", vectorStr)))
        }).
        Limit(limit).
        All(ctx)
        
    return memories, err
}
```

## 🔌 API Architecture

### RESTful Design Principles

The API follows **REST conventions** with consistent resource naming:

```
GET    /api/chat                 # List chats
POST   /api/chat                 # Create chat
GET    /api/chat/{id}            # Get specific chat
PUT    /api/chat/{id}            # Update chat
DELETE /api/chat/{id}            # Delete chat

GET    /api/chat/{id}/message    # List messages in chat
POST   /api/chat/{id}/message    # Send message to chat

GET    /api/memory               # List memories
GET    /api/memory/{id}          # Get specific memory
DELETE /api/memory/{id}          # Delete memory

GET    /api/personality          # List personalities
POST   /api/personality          # Create personality
GET    /api/personality/{id}     # Get specific personality
PUT    /api/personality/{id}     # Update personality
DELETE /api/personality/{id}     # Delete personality
```

### OpenAPI Specification

**Comprehensive API documentation** using OpenAPI 3.0:

```yaml
paths:
  /chat:
    post:
      summary: Create a new chat
      description: Creates a new chat session for the authenticated user
      tags: [Chat Management]
      security:
        - bearerAuth: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateChatRequest'
      responses:
        '201':
          description: Chat created successfully
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Chat'
        '400':
          description: Invalid request
        '401':
          description: Unauthorized

components:
  schemas:
    Chat:
      type: object
      required: [id, name, user_id, created_at]
      properties:
        id:
          type: string
          format: uuid
          description: Unique identifier for the chat
        name:
          type: string
          maxLength: 255
          description: Display name for the chat
        model_id:
          type: string
          format: uuid
          description: ID of the AI model used for this chat
        personality_id:
          type: string
          format: uuid
          description: ID of the personality profile for this chat
```

## 🔮 Rituals System

### Reusable Prompt Templates

**Rituals** provide a powerful mechanism for users to save and reuse prompt patterns with keyboard shortcuts:

```go
// Ritual entity
type Ritual struct {
    ID            uuid.UUID `json:"id"`
    Name          string    `json:"name"`
    Description   string    `json:"description"`
    Content       string    `json:"content"`          // The prompt template
    Hotkeys       string    `json:"hotkeys"`          // Keyboard shortcut
    UserID        uuid.UUID `json:"user_id"`
    PersonalityID uuid.UUID `json:"personality_id"`   // Optional personality binding
    CreatedAt     time.Time `json:"created_at"`
    UpdatedAt     time.Time `json:"updated_at"`
}

// Enrich message with ritual content
func (a *Agent) EnrichUserMessageWithRituals(ctx context.Context, userID uuid.UUID, message *models.ChatMessage) (string, error) {
    ritualIds := GetRitualIds(message.Rituals)
    
    rituals, err := a.ds.GetRitualsByIDs(ctx, userID, ritualIds)
    if err != nil {
        return message.Message, err
    }
    
    // Append ritual content to message
    message.Message += FormatRituals(rituals)
    return message.Message, nil
}
```

### Ritual Use Cases

- **Code Review Templates** - "Review this code for security vulnerabilities and performance issues"
- **Content Editing** - "Edit this text for clarity, grammar, and style"
- **Email Writing** - "Write a professional email with [context]"
- **Document Analysis** - "Summarize this document and extract key insights"
- **Translation** - "Translate the following text to [language]"

### Keyboard Shortcuts

Users can invoke rituals via configurable hotkeys in the frontend, automatically injecting the ritual content into their message context.

## 📎 File Attachment System

### Comprehensive File Support

The application supports **30+ file types** with intelligent handling:

```go
// File type detection and classification
type FileTypeInfo struct {
    ContentType   string  // MIME type
    Extension     string  // File extension
    VectorSupport bool    // Can be indexed for semantic search
}

var supportedTypes = []string{
    // Documents
    ".pdf", ".doc", ".docx", ".md", ".txt", ".tex",
    
    // Spreadsheets & Data
    ".xlsx", ".csv", ".json", ".xml",
    
    // Code
    ".py", ".js", ".ts", ".go", ".java", ".c", ".cpp", 
    ".cs", ".rb", ".php", ".sh", ".html", ".css",
    
    // Images
    ".png", ".jpg", ".jpeg", ".gif",
    
    // Archives
    ".zip", ".tar", ".tgz",
    
    // Presentations
    ".pptx"
}
```

### File Processing Pipeline

```go
// Upload flow
func (a *Agent) UploadFileAttachment(ctx context.Context, userID uuid.UUID, file io.Reader, fileName string) (string, error) {
    // 1. Detect file type
    fileTypeInfo, err := fileutils.GetFileType(fileName)
    
    // 2. Upload to OpenAI
    storedFile, err := a.oaiClient.Files.New(ctx, openai.FileNewParams{
        File:    openai.File(file, fileName, fileTypeInfo.ContentType),
        Purpose: openai.FilePurposeUserData,
    })
    
    // 3. Add to vector store (if supported)
    if fileTypeInfo.VectorSupport {
        _, err = a.oaiClient.VectorStores.Files.New(ctx, vectorStoreID, openai.VectorStoreFileNewParams{
            FileID: storedFile.ID,
            Attributes: map[string]string{
                "user_id":     userID.String(),
                "entity_type": "file_attachment",
            },
        })
    }
    
    return storedFile.ID, nil
}
```

### AI-Generated File Handling

The system automatically captures and stores files generated by AI:

- **Image Generation** - PNG images from DALL-E
- **Code Interpreter Output** - Files created during code execution
- **Data Analysis Results** - Charts, CSVs, processed data

```go
func (a *Agent) saveMessageAttachments(ctx context.Context, userID, chatMessageID uuid.UUID, resp *responses.Response) error {
    for _, output := range resp.Output {
        switch output.Type {
        case "image_generation_call":
            a.saveImageAttachment(ctx, userID, chatMessageID, output.AsImageGenerationCall())
        case "message", "code_interpreter_call":
            a.saveInterpreterAttachments(ctx, userID, chatMessageID, output.Content)
        }
    }
    return nil
}
```

## 🛠️ AI Tools Architecture

### Tool Integration

The AI assistant has access to multiple powerful tools provided by OpenAI:

```go
func getChatTools(userID, chatID, personalityID uuid.UUID, attachments []*models.FileAttachment) []responses.ToolUnionParam {
    return []responses.ToolUnionParam{
        // Web Search
        {OfWebSearchPreview: &responses.WebSearchToolParam{
            Type: "web_search_preview",
        }},
        
        // Image Generation
        {OfImageGeneration: &responses.ToolImageGenerationParam{
            Type: "image_generation",
        }},
        
        // Code Interpreter with attachments
        {OfCodeInterpreter: &responses.ToolCodeInterpreterParam{
            Container: responses.ToolCodeInterpreterContainerUnionParam{
                OfCodeInterpreterContainerAuto: &responses.ToolCodeInterpreterContainerCodeInterpreterContainerAutoParam{
                    Type:    "auto",
                    FileIDs: attachmentIDs,  // User's uploaded files
                },
            },
            Type: "code_interpreter",
        }},
        
        // File Search with filters
        {OfFileSearch: &responses.FileSearchToolParam{
            VectorStoreIDs: []string{os.Getenv("VECTOR_STORE_ID")},
            Filters:        buildFilters(userID, chatID, personalityID),
            Type:           "file_search",
        }},
    }
}
```

### Tool Capabilities

**Web Search**
- Real-time internet information retrieval
- Current events, news, and facts
- Research augmentation

**Image Generation**
- DALL-E integration for creating images
- Automatic saving to chat messages
- Base64 encoded for display

**Code Interpreter**
- Execute Python code in sandboxed environment
- Analyze data from uploaded files
- Generate visualizations and reports
- Process CSV, Excel, and other data formats

**File Search**
- Vector similarity search across uploaded documents
- Filtered by user, chat, or personality
- Semantic retrieval of relevant information

## ⚙️ Background Processing Architecture

### Job System

**Asynchronous processing** for AI operations using a job queue pattern:

```go
// Job entity for tracking background tasks
type Job struct {
    ID        uuid.UUID    `json:"id"`
    Type      models.JobType `json:"type"`
    Status    models.JobStatus `json:"status"`
    Input     interface{}  `json:"input"`
    Output    interface{}  `json:"output,omitempty"`
    Error     *string      `json:"error,omitempty"`
    UserID    uuid.UUID    `json:"user_id"`
    ChatID    *uuid.UUID   `json:"chat_id,omitempty"`
    CreatedAt time.Time    `json:"created_at"`
    UpdatedAt time.Time    `json:"updated_at"`
}

// Job processing workflow
func (ms *MessageService) SendMessage(chatID string, request SendMessageRequest) Observable<SendMessageResponse> {
    return this.http.post<SendMessageResponse>(`${this.apiUrl}/${chatID}/message`, request).pipe(
        tap(response => {
            // Start polling the background job
            this.jobService.pollJob(response.job_id, chatID).subscribe();
        })
    );
}

// Job polling service
func (js *JobService) pollJob(jobID: string, chatID: string): Observable<Job> {
    return timer(0, 2000).pipe(  // Poll every 2 seconds
        switchMap(() => this.getJob(jobID)),
        takeWhile(job => job.status === 'pending' || job.status === 'running', true),
        tap(job => {
            if (job.status === 'completed') {
                // Update UI with results
                this.messageService.loadMessages(chatID);
            }
        })
    );
}
```

### AI Agent Integration

**AI processing** with OpenAI integration and memory augmentation:

```go
type Agent struct {
    client     *openai.Client
    datastore  AgentDatastore
    logger     *zap.Logger
}

func (a *Agent) HandleUserMessage(ctx context.Context, userID, chatID uuid.UUID, message string) error {
    // 1. Store user message
    userMsg := &models.ChatMessage{
        Message: message,
        Origin:  models.MessageOriginUser,
        ChatID:  chatID,
    }
    
    // 2. Get relevant memories using semantic search
    memories, err := a.getMemories(ctx, userID, chatID, message)
    if err != nil {
        a.logger.Error("failed to get memories", zap.Error(err))
    }
    
    // 3. Build context with memory augmentation
    messages := a.buildConversationContext(ctx, chatID, memories, message)
    
    // 4. Generate AI response
    response, err := a.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
        Messages: openai.F(messages),
        Model:    openai.F(openai.ChatModelGPT4),
    })
    
    // 5. Store assistant response
    assistantMsg := &models.ChatMessage{
        Message: response.Choices[0].Message.Content,
        Origin:  models.MessageOriginAssistant,
        ChatID:  chatID,
    }
    
    // 6. Extract and store new memories
    go a.extractMemories(ctx, userID, chatID, message, assistantMsg.Message)
    
    return nil
}
```

## 🔒 Security Architecture

### Authentication Flow

**JWT-based authentication** with access and refresh tokens:

```
┌─────────────┐    POST /login    ┌─────────────┐
│   Client    │ ──────────────► │   Server    │
│             │                  │             │
│             │ ◄────────────── │             │
└─────────────┘  Access Token   └─────────────┘
                 Refresh Token   
                 
┌─────────────┐  Authorization   ┌─────────────┐
│   Client    │ ──────────────► │   Server    │
│             │  Bearer <token>  │             │
│             │ ◄────────────── │             │
└─────────────┘   API Response  └─────────────┘

┌─────────────┐ POST /refresh   ┌─────────────┐
│   Client    │ ──────────────► │   Server    │
│             │ Refresh Token   │             │
│             │ ◄────────────── │             │
└─────────────┘ New Access Token └─────────────┘
```

### Authorization Patterns

**Role-based access control** with resource ownership:

```go
// Authorization middleware
func (h *Handler) authorizeResourceAccess(userID, resourceOwnerID uuid.UUID) error {
    if userID != resourceOwnerID {
        return datastore.ErrUnauthorized
    }
    return nil
}

// Resource ownership enforcement
func (d *Datastore) GetChat(ctx context.Context, userID, chatID uuid.UUID) (*models.Chat, error) {
    chat, err := d.dbClient.Chat.Query().
        Where(
            entchat.ID(chatID),
            entchat.HasOwnerWith(user.ID(userID)),  // Enforce ownership
        ).
        Only(ctx)
    
    if ent.IsNotFound(err) {
        return nil, ErrChatNotFound
    }
    return toChatModel(chat), err
}
```

### Data Protection

**Sensitive data handling** and validation:

```go
// Password hashing with bcrypt
func HashPassword(password string) (string, error) {
    bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    return string(bytes), err
}

// Input validation and sanitization
func validateCreateChatRequest(req *models.CreateChatRequest) error {
    if len(strings.TrimSpace(req.Name)) == 0 {
        return errors.New("chat name is required")
    }
    if len(req.Name) > 255 {
        return errors.New("chat name too long")
    }
    return nil
}

// Sensitive field marking in Ent
field.String("password_hash").
    Sensitive().        // Hide from queries/logs
    NotEmpty()
```

## 🚀 Deployment Architecture

### Multiple Deployment Models

The application supports three deployment models:

1. **Docker Compose** - Local development and simple deployments
2. **Standalone Server** - Traditional VPS/bare metal deployment
3. **Cloud containers** - Any container platform (ECS/Fargate, Cloud Run, Kubernetes)

### Cloud Deployment

The API ships as a standard container (see `Dockerfile`), so any container
platform works: ECS/Fargate, Cloud Run, Kubernetes, Fly.io, or a plain VM
running Docker Compose. A typical cloud topology is a load balancer in front
of one or more API containers, a managed Postgres instance with the pgvector
extension, object storage for file attachments (`S3_FILE_BUCKET`), and a CDN
in front of the static frontend build.

Infrastructure-as-code for the maintainers' hosted deployment lives outside
this repository and is not required to run the app.

### Environment Configuration

**Environment-based configuration** for different deployment stages:

```go
type Config struct {
    Environment   string
    Port         string
    DatabaseURL  string
    OpenAIAPIKey string
    JWTSecret    string
    RedisURL     string
}

func LoadConfig() *Config {
    return &Config{
        Environment:   getEnv("ENV", "development"),
        Port:         getEnv("SERVER_PORT", "8080"),
        DatabaseURL:  mustGetEnv("DATABASE_URL"),
        OpenAIAPIKey: mustGetEnv("OPENAI_API_KEY"),
        JWTSecret:    mustGetEnv("JWT_SECRET"),
        RedisURL:     getEnv("REDIS_URL", ""),
    }
}
```

### Container Architecture

**Docker containerization** for consistent deployments:

```dockerfile
# Multi-stage build for Go backend
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/api-server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
EXPOSE 8080
CMD ["./main"]

# Angular frontend container
FROM node:20-alpine AS angular-builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build --prod

FROM nginx:alpine
COPY --from=angular-builder /app/dist /usr/share/nginx/html
EXPOSE 80
```

### Database Migrations

**Automated database migrations** using Ent:

```go
// Migration strategy
func runMigrations(client *ent.Client) error {
    ctx := context.Background()
    
    // Create migration files
    err := client.Schema.WriteTo(ctx, os.Stdout)
    if err != nil {
        return fmt.Errorf("failed to write schema: %w", err)
    }
    
    // Apply migrations
    err = client.Schema.Create(ctx)
    if err != nil {
        return fmt.Errorf("failed to create schema: %w", err)
    }
    
    return nil
}
```

## 📊 Performance Considerations

### Database Optimization

**Query optimization** and indexing strategies:

```go
// Composite indexes for common queries
func (Chat) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("user_id", "created_at"),        // List user chats by date
        index.Fields("user_id", "last_message_time"), // Recent chats first
        index.Fields("model_id"),                     // Filter by model
        index.Fields("personality_id"),               // Filter by personality
    }
}

// Optimized queries with proper joins
func (d *Datastore) ListChats(ctx context.Context, userID uuid.UUID) ([]*models.Chat, error) {
    return d.dbClient.Chat.Query().
        Where(entchat.HasOwnerWith(user.ID(userID))).
        WithModel().          // Preload model relationship
        WithPersonality().    // Preload personality relationship  
        Order(ent.Desc(entchat.FieldLastMessageTime)).
        All(ctx)
}
```

### Caching Strategy

**Memory and Redis caching** for frequently accessed data:

```go
// In-memory caching for models
type ModelCache struct {
    models []models.Model
    mutex  sync.RWMutex
    ttl    time.Duration
}

func (mc *ModelCache) GetModels() []models.Model {
    mc.mutex.RLock()
    defer mc.mutex.RUnlock()
    return mc.models
}

// Redis caching for user sessions
func (a *AuthService) CacheUserSession(userID uuid.UUID, sessionData interface{}) error {
    key := fmt.Sprintf("session:%s", userID)
    return a.redis.Set(key, sessionData, time.Hour*24).Err()
}
```

### Frontend Performance

**Angular optimization** strategies:

```typescript
// OnPush change detection for performance
@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  // ...
})
export class ChatPageComponent {
  readonly session = inject(ChatSessionService);
  readonly groups = computed(() => groupMessages(this.session.messages()));
}

// Lazy loading for feature modules
const routes: Routes = [
  {
    path: 'memory',
    loadComponent: () => import('./features/memory/memory-list.component')
      .then(m => m.MemoryListComponent)
  }
];
```

## 🧪 Testing Architecture

### Backend Testing

**Unit and integration testing** strategies:

```go
// Unit tests with mocks
func TestHandler_CreateChat(t *testing.T) {
    mockStore := &MockDatastore{}
    mockLogger := zap.NewNop()
    handler := NewHandler(mockStore, mockLogger)
    
    // Setup mock expectations
    mockStore.On("CreateChat", mock.Anything, mock.Anything, mock.Anything).
        Return(&models.Chat{ID: uuid.New(), Name: "Test Chat"}, nil)
    
    // Test the handler
    req := httptest.NewRequest("POST", "/chat", strings.NewReader(`{"name":"Test Chat"}`))
    w := httptest.NewRecorder()
    
    handler.CreateChat(w, req)
    
    assert.Equal(t, http.StatusCreated, w.Code)
    mockStore.AssertExpectations(t)
}

// Integration tests with test database
func TestIntegration_ChatWorkflow(t *testing.T) {
    client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
    defer client.Close()
    
    // Test full workflow
    ctx := context.Background()
    
    // Create user
    user := client.User.Create().
        SetUsername("testuser").
        SetEmail("test@example.com").
        SaveX(ctx)
    
    // Create chat
    chat := client.Chat.Create().
        SetName("Test Chat").
        SetOwner(user).
        SaveX(ctx)
    
    assert.Equal(t, "Test Chat", chat.Name)
    assert.Equal(t, user.ID, chat.Edges.Owner.ID)
}
```

### Frontend Testing

**Angular testing** with TestBed and mocks:

```typescript
describe('ChatPageComponent', () => {
  let component: ChatPageComponent;
  let fixture: ComponentFixture<ChatPageComponent>;
  let mockChatService: { listChats: Mock; createChat: Mock };

  beforeEach(async () => {
    const chatServiceSpy = { listChats: vi.fn(), createChat: vi.fn() };

    await TestBed.configureTestingModule({
      imports: [ChatPageComponent],
      providers: [
        { provide: ChatService, useValue: chatServiceSpy }
      ]
    }).compileComponents();

    mockChatService = TestBed.inject(ChatService) as unknown as { listChats: Mock; createChat: Mock };
  });

  it('should create chat', () => {
    mockChatService.createChat.mockReturnValue(of({ id: '1', name: 'Test Chat' }));

    component.startConversation();
    
    expect(mockChatService.createChat).toHaveBeenCalled();
  });
});
```

## 🔄 Development Workflow

### Code Generation

**Automated code generation** using Ent:

```bash
# Generate database code
go generate ./ent

# Generate OpenAPI client
openapi-generator generate -i openapi.yaml -g typescript-angular -o ./web/src/app/generated
```

### Build Pipeline

**Continuous integration** workflow:

```yaml
# .github/workflows/ci.yml
name: CI Pipeline
on: [push, pull_request]

jobs:
  backend:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: pgvector/pgvector:pg16
        env:
          POSTGRES_PASSWORD: postgres
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v3
        with:
          go-version: 1.24
      - run: go mod download
      - run: go test ./...
      - run: go build ./cmd/api-server
  
  frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-node@v3
        with:
          node-version: 20
      - run: cd web/app && npm ci
      - run: cd web/app && npm test
      - run: cd web/app && npm run build
```

---

## 📊 Architecture Summary

This architecture supports **scalability**, **maintainability**, and **extensibility** while following industry best practices for full-stack application development.

### Key Architectural Decisions

✅ **Clean Architecture** - Clear separation between HTTP, Service, and Data layers  
✅ **Repository Pattern** - Abstract data access for testability  
✅ **Dependency Injection** - Loosely coupled components  
✅ **JWT Authentication** - Stateless, scalable auth with refresh tokens  
✅ **PostgreSQL + pgvector** - Powerful relational database with vector similarity search  
✅ **OpenAI Integration** - Multiple AI tools (web search, code interpreter, image generation, file search)  
✅ **File Management** - Comprehensive support for 30+ file types with vector indexing  
✅ **Rituals System** - Reusable prompt templates with keyboard shortcuts  
✅ **Background Jobs** - Asynchronous AI processing with status tracking  
✅ **Multi-Deployment** - Docker or standalone
✅ **CI/CD Pipelines** - Three independent pipelines with OIDC authentication  
✅ **Version Management** - Immutable image versions with instant rollback  

### Technology Highlights

**Backend:** Go 1.24.6 • Gorilla Mux • Ent ORM • OpenAI SDK • Zap Logging  
**Frontend:** Angular 20 • TypeScript 5.8 • Tailwind CSS 4 • RxJS 7.8  
**Database:** PostgreSQL 16 + pgvector extension for vector similarity  
**Infrastructure:** Docker • PostgreSQL 16 + pgvector • S3-compatible object storage  
**DevOps:** GitHub Actions • Docker Compose  

### Architecture Benefits

**For Developers:**
- Fast iteration with hot reload (frontend) and quick builds (backend)
- Comprehensive testing support (unit, integration, frontend)
- Type safety across the stack (Go + TypeScript)
- Clear code organization and patterns
- Extensive documentation (ADRs, architecture docs, OpenAPI spec)

**For Operations:**
- Multiple deployment options (Docker, standalone)
- Instant rollback capability 
- Infrastructure as Code with Terraform
- Automated CI/CD with three independent pipelines
- No secrets in GitHub (OIDC authentication)
- Comprehensive logging and monitoring hooks

**For Users:**
- Fast, responsive UI with Angular 20
- Rich AI capabilities (chat, file analysis, code execution, image generation)
- Semantic memory with intelligent recall
- Custom personalities and reusable rituals
- File upload and management (30+ types)
- Secure authentication with JWT

### Extensibility Points

The architecture is designed for easy extension:

1. **New AI Tools** - Add to `getChatTools()` function
2. **New File Types** - Extend `fileTypes` map in `fileutils`
3. **New Entities** - Add Ent schema and generate ORM code
4. **New API Endpoints** - Follow handler pattern in `internal/handlers`
5. **New Frontend Features** - Add feature module in `src/app/features`
6. **New Deployment Targets** - Application auto-detects environment

---

**Built with ❤️ following modern software engineering practices**

For more details:
- See `docs/ARCHITECTURE_SUMMARY.md` for the system architecture summary
- See `openapi.yaml` for complete API specification
