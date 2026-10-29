<div align="center">

<img src="docs/assets/whatiff-logo.png" alt="WhatIff" width="120" />

# WhatIff

**An open-source, self-hostable AI assistant where your continuity and memory are not tied to a single model provider.**

WhatIff keeps the persistent AI state in your stack: conversations, long-term memory, personalities, tools, and scheduled agents. The model that generates the next response is interchangeable, so you can move between supported providers without making any one of them the owner of your AI's continuity.

[![Website](https://img.shields.io/badge/Website-whatiff.chat-6D5AE6)](https://whatiff.chat)
[![Blog](https://img.shields.io/badge/Blog-Imagination_Foundry-FF6719?logo=substack&logoColor=white)](https://imaginationfoundry.substack.com/)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![Angular](https://img.shields.io/badge/Angular-22-DD0031?logo=angular&logoColor=white)](web/app)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-pgvector-336791?logo=postgresql&logoColor=white)](https://github.com/pgvector/pgvector)
[![codecov](https://codecov.io/gh/theimaginaryfoundation/what-iff/branch/main/graph/badge.svg)](https://codecov.io/gh/theimaginaryfoundation/what-iff)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

</div>

---

WhatIff is the open-source core that powers [whatiff.chat](https://whatiff.chat), a Go API server and an Angular web app you can run yourself. It owns the continuity layer independently of the model provider: semantic memory, personalities, conversation state, tools, and recurring work stay with the WhatIff deployment while supported models can be selected per user and per chat. Bring your own model API keys and host the whole thing on your own infrastructure.

Want the story behind it? Read the [Imagination Foundry blog](https://imaginationfoundry.substack.com/).

## ✨ Features

### 🤖 Multi-provider chat
- Chat with models from **OpenAI, Anthropic, Google (Gemini), Z.ai (GLM), Mistral, DeepSeek, Qwen, and Xiaomi** — configurable per user and per chat
- Streaming responses with Markdown rendering and an emoji picker
- Multiple chat sessions with sidebar navigation, inline renaming, and per-chat settings

### 🧠 Semantic memory
- Long-term memory stored as vector embeddings (**pgvector**) and recalled by conversation context
- Filter and search memories by date, scope, chat, and content
- Memory merging and compaction with checkpoints so context stays coherent over time

### 🎭 Personalities
- Define reusable AI personas with custom system prompts and behavior
- Switch personalities per chat or set a default; attach reference documents to a persona
- Full create/edit/organize UI

### 🔮 Rituals
- Reusable prompt templates you can invoke with customizable keyboard shortcuts
- Bind a ritual to a personality and auto-enrich messages with its context

### 🛠️ Tools & agents
- **File attachments** with 30+ supported types, plus semantic search over uploaded documents
- **Code interpreter**, **image generation**, and **web search** available to the assistant
- **Agents** — scheduled and background AI jobs with a distributed-lock-aware scheduler
- AI-generated files saved straight back into the conversation

### 🔗 Integrations
- **Remote MCP connectors** with per-chat activation controls
- **Webhook API tokens** (stored as one-way hashes) with static bearer auth isolated from session JWTs, each limited to the scopes it was created with (`messages:write`, `chat:read`)
- Webhook trigger modes for existing chats: `user`, `assistant` (write-only), and `background` (async agent trigger)
- Webhook read routes (scope `chat:read`): threads (filterable by persona), persona names, messages, and job status; see the [webhooks guide](https://whatiff.chat/guides/webhooks.html) and the [integration reference](docs/integrations.md)

### 🔧 Built for operators
- OpenAPI specification and structured logging (Zap)
- Ent ORM with automatic, non-destructive database migrations
- Docker & Docker Compose for local development
- GitHub Actions CI

## 🏗️ Tech stack

| Layer | Technologies |
|-------|-------------|
| **Backend** | Go 1.27 · Gorilla Mux · Ent ORM · PostgreSQL 16 + pgvector · JWT auth · Zap |
| **Frontend** | Angular 22 · TypeScript 5 · Tailwind CSS 4 · RxJS · ngx-markdown |
| **AI** | OpenAI, Anthropic, Google, Z.ai, Mistral, DeepSeek, Qwen, Xiaomi (bring your own keys) |
| **Infra** | Docker & Docker Compose · GitHub Actions |

## 🚀 Quick start (Docker)

The fastest way to run the full stack locally is Docker Compose.

**Prerequisites:** Docker, and an OpenAI API key, which you add in the app.

```bash
# Start the stack. On first run it generates this install's secrets, runs
# migrations, and seeds the model catalog.
docker compose up --build
```

Then open http://localhost:8081, create an account, and add your OpenAI key when the
app asks (it lives in **Profile & Settings → API keys**, where you can add other
providers' keys too). Keys belong to your account and are stored encrypted; there is
nothing to put in `.env`.

The JWT and token-encryption secrets are generated once per install and kept in a
Docker volume; nothing is shipped in the repository. Values you set in your shell or
`.env` take precedence. `docker compose down -v` deletes them along with the database.

| Service | URL |
|---------|-----|
| Frontend | http://localhost:8081 |
| API | http://localhost:8080 |
| PostgreSQL | localhost:5432 |

**Models on this computer.** The API runs in a container, where `localhost` is the
container itself, so it reaches a model server on your machine (Ollama, LM Studio) as
`host.docker.internal`. Two things on the host have to allow that:

- **The server has to listen beyond loopback.** Ollama listens on `127.0.0.1` by default;
  start it with `OLLAMA_HOST=0.0.0.0`. In LM Studio, turn on serving on the local network.
- **A host firewall has to let the Docker network in** on that port (11434 for Ollama,
  1234 for LM Studio). Firewalls such as ufw drop that traffic by default.

> Compose is for **local development only**. Its defaults (`DESTRUCTIVE_MIGRATION=false`, `DB_SSL_MODE=disable`, no secret fallbacks) are deliberately conservative — every deployed environment must set its own real secrets and connection settings explicitly.

## 🧑‍💻 Local development

Run the backend and frontend directly for a faster inner loop.

**Backend** (Go 1.27+, a running PostgreSQL 16 with pgvector):

```bash
go run ./cmd/api-server
# Connects to Postgres, enables pgvector, runs non-destructive migrations
# (AUTO_MIGRATE=true), seeds a default model, and serves on :8080.
```

**Frontend** (Node.js 20+):

```bash
cd web/app
npm install
npm start          # Angular dev server on :4200, proxying the API on :8080
```

## 🔧 Configuration

Configuration is via environment variables. The essentials:

| Variable | Purpose |
|----------|---------|
| `OPENAI_API_KEY` and other provider keys | Not used by default: each account adds its own keys in the app. A deployment that supplies keys for everyone binds `providerkeys.New` to these instead |
| `JWT_SECRET`, `JWT_REFRESH_SECRET` | JWT access/refresh token signing (≥32 chars). Generated automatically under Docker Compose |
| `TOKEN_ENCRYPTION_SECRET` | Encryption key for at-rest secrets (≥32 chars). Generated automatically under Docker Compose |
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` | PostgreSQL connection |
| `SERVER_HOST`, `SERVER_PORT` | API bind address (default `localhost:8080`) |
| `ENVIRONMENT` | `development` \| `staging` \| `production` |
| `AUTO_MIGRATE` | Run migrations on boot (`true`/`false`) |
| `LOCAL_SERVER_HOST` | The name a model server on this computer is reached by: `localhost` by default, `host.docker.internal` in the container image |
| `DESTRUCTIVE_MIGRATION` | Allow dropping columns/indexes during auto-migrate — keep `false` in deployed envs |

See `.env.example` and `docker-compose.yml` for the full set.

## 📁 Project structure

```
what-iff/
├── cmd/api-server/       # Application entry point
├── internal/
│   ├── agent/            # Tool-using AI agent, tools, and file processing
│   ├── agentjobs/        # Scheduled & background agent jobs (scheduler)
│   ├── auth/             # JWT, password hashing, token utilities
│   ├── datastore/        # Database operations (repository pattern over Ent)
│   ├── handlers/         # HTTP request handlers
│   ├── memoryutil/       # Semantic memory helpers
│   ├── metering/         # Usage-metering seam (no-op in the open-source build)
│   ├── middleware/       # Auth, CORS, logging
│   ├── models/           # Data transfer objects
│   ├── providers/        # Model provider integrations
│   └── server/           # HTTP server wiring & routing
├── ent/schema/           # Ent database schema (generated code is not committed)
├── web/app/  # Angular frontend
├── docs/                 # Architecture and reference docs
└── openapi.yaml          # OpenAPI specification
```

## 📚 Documentation

- **[ARCHITECTURE.md](ARCHITECTURE.md)** — system design, patterns, and internals
- **[openapi.yaml](openapi.yaml)** — full REST API specification
- **[Mullet mode](docs/guides/mullet-mode.md)** — keep a copy of your whatiff.chat threads on your own server and continue them there
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — how to contribute
- **[SECURITY.md](SECURITY.md)** — reporting vulnerabilities

## 🤝 Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) and our [Code of Conduct](CODE_OF_CONDUCT.md) before opening a pull request. Issues and feature ideas are equally appreciated.

## 📄 License

Licensed under the [Apache License 2.0](LICENSE).

<div align="center">
<br />
Built by the team behind <a href="https://whatiff.chat">WhatIff</a> · <a href="https://imaginationfoundry.substack.com/">Blog</a>
</div>
