# Repository Guidelines

## Project Overview

WeKnora is a multi-tenant, RAG-powered knowledge-base platform by Tencent. It ingests documents (PDF, DOCX, XLSX, PPT, EPUB, MD, MHTML, images, URLs, RSS, Notion, Feishu, Yuque), chunks/embeds them into pluggable vector stores, and serves Q&A through a web UI, embeddable chat widgets, IM channels (WeCom, Feishu, Slack, Telegram, DingTalk, Mattermost, QQ), a CLI, and MCP tool servers. It supports custom ReAct agents with sandboxed skill execution, multi-turn conversations, hybrid search, GraphRAG, and multimodal image analysis.

**Polyglot monorepo**: Go 1.26 backend + Vue 3 frontend + Python gRPC doc-parsing sidecar + Python MCP server + WeChat miniprogram + separate Go CLI module.

## Architecture & Data Flow

### Layered Backend (Go)

```
cmd/server/main.go
  → container.BuildContainer() (go.uber.org/dig DI)
  → runStartupBootstrap() (best-effort one-shot hooks)
  → router.NewRouter(RouterParams{dig.In}) → Gin HTTP server with graceful shutdown
```

Classic layered architecture with interface-driven contracts:

| Layer | Directory | Responsibility |
|---|---|---|
| **Handler** | `internal/handler/` | Gin HTTP handlers — thin: parse request → call service → write JSON. One file per resource. DTOs in `handler/dto/`. |
| **Service** | `internal/application/service/` | Business logic, split per concern (e.g. `knowledge_create.go`, `knowledge_process.go`). Sub-packages: `chat_pipeline/` (RAG plugins), `file/`, `memory/`, `retriever/`. |
| **Repository** | `internal/application/repository/` | GORM-backed data access, one file per aggregate. Sub-packages: `retriever/<provider>/` (vector engines), `memory/neo4j/`. |
| **Interfaces** | `internal/types/interfaces/` (~45 files) | **Single source of truth** for all service/repository/infra interface contracts. Implementations depend on interfaces, not concrete types. |

### Dependency Injection (go.uber.org/dig)

`internal/container/container.go` `BuildContainer()` registers every provider in strict layered order: core infra → repositories → MCP manager → business services → chat pipeline plugins → HTTP handlers → router.

- **Interface binding**: `dig.As(new(interfaces.X))` binds a constructor result to an interface.
- **Named registrations**: `dig.Name("chunkExtractor")` for multiple implementations of the same interface.
- **Multi-dep injection**: `RouterParams` embeds `dig.In` and lists ~50 handler/service deps auto-resolved by dig.
- **Startup side-effects**: `container.Invoke` for schedulers, cleanup hooks, plugin registration (no return value).
- **Conditional Redis**: If `REDIS_ADDR` is set → asynq task queue; else → `NewSyncTaskExecutor` (inline goroutines, Lite mode).

### Key Internal Packages

| Package | Purpose |
|---|---|
| `internal/agent/` | ReAct agent engine (`engine.go`): think→act→observe loop, tool registry, MCP tool approval gate, memory. Stateless per turn (history rebuilt from DB). |
| `internal/application/service/chat_pipeline/` | Pluggable RAG pipeline: PluginSearch, PluginRerank, PluginWebFetch, PluginMerge, PluginDataAnalysis, PluginChatCompletion, PluginFilterTopK, PluginQueryUnderstand, PluginLoadHistory, PluginExtractEntity, PluginWikiBoost, PluginMemory. |
| `internal/config/` | Viper config loading with `${ENV_VAR}` interpolation. |
| `internal/container/` | DI wiring + `engine_factory.go` (dynamic vector engine creation). |
| `internal/datasource/` | External sync connectors (Feishu, Notion, RSS, Yuque) + Scheduler. |
| `internal/errors/` | `AppError{Code, Message, Details, HTTPCode}` — structured error with domain-specific codes (1000–1010 general, 2000s tenant, 2100s agent, 2200s vector store). |
| `internal/event/` | In-process EventBus for agent event streaming. |
| `internal/im/` | IM adapters: dingtalk, feishu, mattermost, qqbot, slack, telegram, wechat, wecom. |
| `internal/infrastructure/` | Chunker, docparser (gRPC client to docreader), web_fetch, web_search. |
| `internal/mcp/` | MCP client manager + OAuth manager for agent tool calls. |
| `internal/middleware/` | Auth (JWT+API key), RBAC, KB access, rate limiting, audit, language, error handler. |
| `internal/models/` | LLM clients: `chat/`, `embedding/`, `rerank/`, `vlm/`, `asr/`, `provider/` (per-provider adapters: OpenAI, Anthropic, Gemini, DeepSeek, Qwen, Zhipu, Volcengine, Azure, etc.). |
| `internal/router/` | Route registration + RBAC guards + asynq task queue wiring. |
| `internal/types/` | GORM domain models (~85 files) + `interfaces/` contracts. |

### Request Flow

```
HTTP request
  → middleware: RequestID → Language → Logger → Recovery → ErrorHandler
  → Auth (JWT Bearer or X-API-Key, resolves user/tenant/role)
  → RBAC guard (RequireRole / RequireOwnershipOrRole, honors EnableRBAC flag)
  → /api/v1/<resource> handler
  → service interface (injected via dig)
  → repository (GORM) / retriever engine / file service / LLM client
  → JSON response envelope: {success, data} or {success:false, error:{code, message, details}}
```

### Document Processing Pipeline

```
Upload → FileService (storage backend) → asynq task queue (Redis) or sync executor (Lite)
  → docreader gRPC (parse to markdown + extract images)
  → Chunker (split with configurable separators/parser rules)
  → Embedding (batched, provider-specific)
  → VectorStore write (CompositeRetrieveEngine)
  → optional: VLM image description, graph extraction (Neo4j), FAQ question generation
```

## Key Directories

### Backend (Go)

| Path | Purpose |
|---|---|
| `cmd/server/` | Primary HTTP server binary. `main.go` (entry), `bootstrap.go` (system admin promotion), `listen.go` (port retry), `signals_*.go`. |
| `cmd/desktop/` | Wails v2 desktop app — embeds the same Go backend + serves Vue frontend in a webview. |
| `cmd/download/duckdb/` | Standalone DuckDB native library downloader. |
| `client/` | Go SDK used by the `weknora` CLI. Dual auth (API key or JWT), typed resource methods. |
| `internal/` | All backend application code (see Architecture above). |
| `migrations/` | SQL migrations: `versioned/` (PostgreSQL, 000000–000064+), `sqlite/` (Lite), `mysql/`, `paradedb/`. Paired `.up.sql`/`.down.sql`, 6-digit zero-padded. |
| `config/` | `config.yaml`, `builtin_models.yaml`, `builtin_agents.yaml`, `agent_type_presets.yaml`, `prompt_templates/*.yaml`. |
| `docker/` | Dockerfiles: `Dockerfile.app` (multi-stage Go), `Dockerfile.docreader` (Python), `Dockerfile.sandbox`, `Dockerfile.odl-hybrid`. |
| `helm/` | Helm chart for K8s deployment. |
| `scripts/` | Orchestration: `start_all.sh`, `dev.sh`, `migrate.sh`, `build_images.sh`, `get_version.sh`, `cloud-image/`. |
| `tests/` | Only miniprogram node tests. Backend tests are colocated with source. |

### Frontend (Vue)

| Path | Purpose |
|---|---|
| `frontend/src/main.ts` | SPA entry — registers Pinia + router + i18n + TDesign. |
| `frontend/src/embed-main.ts` | Separate embed.html entry (minimal chat widget). |
| `frontend/src/api/` | REST client modules, one folder per domain (agent, auth, chat, knowledge-base, model, system, tenant, etc.). All use `utils/request.ts` (axios). |
| `frontend/src/views/` | Route-level pages: `auth/`, `platform/` (shell), `knowledge/`, `chat/`, `agent/`, `settings/`, `organization/`, `admin/`, `embed/`, `dev/`. |
| `frontend/src/stores/` | 10 Pinia stores (see below). |
| `frontend/src/components/` | ~153 shared components. |
| `frontend/src/composables/` | Vue composables (useTheme, useChatStreamHandler, useEmbedBridge, etc.). |
| `frontend/src/hooks/` | Older-style hooks (coexists with `composables/`). |
| `frontend/src/i18n/locales/` | 4 locale files: `zh-CN.ts` (default), `en-US.ts`, `ru-RU.ts`, `ko-KR.ts` (~6k lines each). |
| `frontend/src/utils/` | ~55 pure helpers (request.ts, chatMarkdownRenderer.ts, security.ts, etc.). |
| `frontend/src/wailsjs/` | Auto-generated Wails desktop bindings — do not edit. |

### Other Services

| Path | Purpose |
|---|---|
| `docreader/` | Python 3.10+ gRPC document-parsing microservice. Parses files → markdown + images + metadata. Separate Docker image. Go backend is the gRPC client. |
| `cli/` | Separate Go module (`github.com/Tencent/WeKnora/cli`). Cobra-based `weknora` CLI with its own `go.mod`. Has `AGENTS.md` (630 lines) + acceptance tests. |
| `mcp-server/` | Python MCP server exposing WeKnora REST API as MCP tools. Separate from CLI's built-in Go MCP server. |
| `miniprogram/` | WeChat Mini Program client. |
| `examples/` | Agent Skills examples (`skills/`). |
| `docs/` | API docs (per-resource `.md`), wiki (VitePress-style), Swagger JSON/YAML (generated). |

## Development Commands

### Backend (Go)

```bash
make build              # go build -o WeKnora ./cmd/server
make run                # build + run
make test               # go test -v ./...
make fmt                # go fmt ./...
make lint               # golangci-lint run (config: .golangci.yml)
make docs               # swag init → ./docs (Swagger)
make build-prod         # CGO_ENABLED=1, ldflags inject version/edition
make build-lite         # SQLite + in-memory queue single binary (WeKnora-lite)
```

### Frontend (Vue)

```bash
cd frontend
pnpm install            # uses pnpm 11.9.0 (declared in devEngines)
pnpm run dev            # vite dev server
pnpm run build          # vite build
pnpm run type-check     # vue-tsc --build
pnpm test               # node --test (Node.js built-in test runner)
```

### Database Migrations

```bash
make migrate-up                   # run pending migrations
make migrate-down                 # rollback last
make migrate-version              # show current version
make migrate-create name=foo      # create new up/down SQL pair
make migrate-force version=N      # force-set version (use -1 to reset)
make migrate-goto version=N       # migrate to specific version
```

Migrations auto-run on startup when `AUTO_MIGRATE != false`. Format: `NNNNNN_descriptive_name.{up,down}.sql` in `migrations/versioned/`.

### Dev Mode (infra via Docker, app+frontend local)

```bash
make dev-start          # start infra (postgres, redis, docreader) via docker-compose.dev.yml
make dev-app            # run Go backend with air hot-reload (.air.toml)
make dev-frontend       # run Vue dev server
make dev-stop           # stop infra
make dev-status         # check infra status
```

### Docker

```bash
make docker-build-all    # build app + docreader + frontend images
make docker-run          # docker-compose up (touches .env if missing)
make docker-stop         # docker-compose down
make docker-restart      # stop -t 60 && up
```

### CLI (separate module)

```bash
cd cli
make test               # go test ./...
make test-coverage      # coverprofile
make lint               # go vet ./...
# contract tests with golden refresh:
go test -update ./acceptance/contract/...
# e2e (gated by build tag):
WEKNORA_E2E_HOST=... WEKNORA_E2E_TOKEN=... go test -tags=acceptance_e2e -v ./acceptance/e2e/...
```

### docreader (Python)

```bash
cd docreader && pytest
```

### MCP Server (Python)

```bash
cd mcp-server && pytest
```

## Code Conventions & Common Patterns

### Go Backend

- **Layered architecture**: handler → service (interface from `internal/types/interfaces/`) → repository. Never import a concrete service implementation; depend on the interface.
- **DI**: All wiring via `go.uber.org/dig` in `internal/container/container.go`. To add a new service: register its constructor in `BuildContainer()`, bind to interface with `dig.As()`.
- **Error handling**: Use `internal/errors.AppError` constructors (`NewBadRequestError`, `NewNotFoundError`, etc.). In handlers, use `c.Error(*AppError)` + `c.Abort()` — never `c.JSON()` for errors (the `ErrorHandler` middleware renders the unified envelope).
- **Domain models**: GORM models in `internal/types/` with `TableName()`, `Value()/Scan()` for JSON columns, `BeforeCreate` UUID hooks.
- **Storage abstraction**: `interfaces.FileService` with providers: `local`, `minio`, `cos`, `tos`, `s3`, `oss`, `ks3`, `obs`. File paths use `provider://` scheme. KB-level `StorageProviderConfig` pins a provider per KB.
- **Vector store abstraction**: `retriever.RetrieveEngineRegistry` implements both `RetrieveEngineRegistry` (env stores via `RETRIEVE_DRIVER`) and `StoreRegistry` (DB-managed `VectorStore` instances). `CompositeRetrieveEngine` wraps per-KB engine selection.
- **LLM providers**: `internal/models/provider/` — one file per provider. `providerAdapter` interface with `baseProvider` default; per-provider overrides for auth, request shaping, thinking mode. `resolveProvider(name, model)` picks the adapter.
- **Agent engine**: `internal/agent/engine.go` — ReAct loop, stateless across turns (history rebuilt from DB). Events via `EventBus` → SSE to client.
- **Routing**: `internal/router/router.go` — `RouterParams` embeds `dig.In` with ~50 deps. Per-domain `Register*Routes(v1, handler, guards)`. RBAC guards honor `cfg.Tenant.EnableRBAC` (log-and-pass when off).
- **Config**: Viper with `AutomaticEnv()` (env vars override config keys, `.` → `_`). `${ENV_VAR}` interpolation in YAML files. `.env` loaded via godotenv.

### Frontend (Vue 3)

- **Composition API**: `<script setup lang="ts">` with `defineProps`/`defineEmits`. No Options API in new code (legacy `knowledge.ts` and `ui.ts` stores are exceptions).
- **State management**: Pinia stores in `frontend/src/stores/`:
  - `auth.ts` — user/tenant/auth/role state (localStorage-hydrated; role values are UI-gating only, server is authority)
  - `settings.ts` — chat/session config (persisted via `settingsStorage.ts`)
  - `chatResources.ts` — tenant-scoped resource cache (60s TTL) for chat selectors
  - `editorResources.ts` — tenant-scoped cache for KB/agent editor dropdowns
  - `organization.ts` — multi-tenant orgs
  - `ui.ts` — global modal/overlay toggles (drives modals mounted in `App.vue`)
  - `menu.ts` — sidebar nav
  - `uploadConfirm.ts`, `commandPalette.ts`, `knowledge.ts` (legacy)
- **API layer**: `frontend/src/api/<domain>/index.ts` — typed functions calling `utils/request.ts` (axios with token refresh + `X-Tenant-ID` header). Streaming via `@microsoft/fetch-event-source`.
- **Component naming**: PascalCase `.vue` for most components; some legacy kebab-case. Views are PascalCase.
- **i18n**: `vue-i18n` composition mode, 4 locales. Keys in nested tree (`menu.*`, `knowledgeBase.*`, `error.*`). Adding a backend error code requires coordinated frontend i18n key.
- **Modals**: Global modals driven by `ui` store (`showSettingsModal`, `showKBEditorModal`), mounted once in `App.vue`.
- **Markdown rendering**: `marked` + `marked-katex-extension` + `highlight.js` + `mermaid` + `dompurify`. Custom renderer in `utils/chatMarkdownRenderer.ts`.
- **Path alias**: `@/*` → `./src/*` (tsconfig.app.json).
- **Desktop/Lite**: `isLiteMode` flag in auth store. `--wails-draggable` attrs on headers. Wails bindings in `src/wailsjs/` (auto-generated, do not edit).

### CLI (Go, separate module)

- **Pattern**: Options struct (flag-bound) + narrow Service interface (duck-typed by `*sdk.Client`) + `NewCmdVerb(f *cmdutil.Factory)` + separate `runVerb()` (test injection point). Lazy-init `f.Client()`/`Secrets()` inside `RunE`.
- **Wire contract**: stdout = data (bare JSON default, `--format json|ndjson|text`, `--jq`); stderr = errors/logs. Typed exit codes (0 success, 2 arg-validation, 3 auth, 4 not_found, 5 input, 6 rate_limited, 7 server/network, 10 confirmation_required, 124 timeout, 130 cancelled).
- **Testing**: Hand-written narrow Service fakes (no gomock/mockery). Contract tests with golden JSON files in `cli/acceptance/testdata/wire/`.

## Important Files

| File | Role |
|---|---|
| `cmd/server/main.go` | Server entry point — builds DI container, starts Gin with graceful shutdown. |
| `internal/container/container.go` | DI wiring — `BuildContainer()` registers all providers in layered order. |
| `internal/router/router.go` | Route registration + middleware chain + RBAC guards. |
| `internal/types/knowledgebase.go` | Central `KnowledgeBase` domain model — config, storage, indexing, VLM, ASR, graph. |
| `internal/types/interfaces/` | All interface contracts (~45 files). **Read these before adding a new service.** |
| `internal/errors/errors.go` | `AppError` + error code constructors. |
| `internal/config/config.go` | Viper config loading with env interpolation. |
| `internal/application/service/retriever/registry.go` | Vector engine registry (env + DB stores). |
| `internal/agent/engine.go` | ReAct agent engine. |
| `internal/models/chat/provider.go` | LLM provider adapter registry. |
| `config/config.yaml` | Main app config (server, conversation, KB defaults, extract). |
| `config/builtin_models.yaml` | Declarative LLM/embedding/rerank model definitions. |
| `config/prompt_templates/*.yaml` | Prompt templates loaded by ID reference at startup. |
| `Makefile` | All build/test/migrate/docker/dev targets. |
| `.env.example` | Full environment variable reference (722 lines). |
| `frontend/src/main.ts` | SPA entry. |
| `frontend/src/utils/request.ts` | Axios instance with auth/token-refresh/tenant headers. |
| `frontend/src/stores/auth.ts` | Auth/tenant/role state. |
| `cli/AGENTS.md` | CLI module's own detailed agent guide (630 lines). |
| `cli/acceptance/contract/wire_test.go` | CLI contract tests with golden JSON. |
| `docreader/proto/docreader.proto` | gRPC contract for document parsing. |

## Runtime/Tooling Preferences

| Requirement | Detail |
|---|---|
| **Go** | 1.26 (go.mod). CGO_ENABLED=1 required for production builds (SQLite, DuckDB, sqlite-vec). |
| **Node/Frontend** | pnpm 11.9.0 (declared in `devEngines`; npm will fail with `EBADDEVENGINES`). Always use `pnpm`, never `npm`. |
| **Python** | 3.10+ for docreader and mcp-server. |
| **DB** | PostgreSQL (ParadeDB image for BM25+vector) primary; SQLite for Lite edition. |
| **Redis** | Optional in Lite (in-memory fallback); required for asynq task queue in standard. |
| **Protobuf** | Production builds set `protoregistry.conflictPolicy=warn` via ldflags (qdrant/milvus conflict). |
| **Swagger** | Generated via `swag init` from Go handler annotations. Served at `/swagger/index.html` (non-release). |
| **Vendored xlsx** | `frontend/packages/xlsx-0.20.2.tgz` — vendored, not from npm registry. |
| **TDesign icons** | Offline guard installed (`installTDesignIconOfflineGuard()`) — no runtime requests to tdesign.gtimg.com. |

## Testing & QA

### Go Tests

- **Layout**: Colocated with source (`internal/<pkg>/*_test.go`, `cmd/*_test.go`, `cli/cmd/<verb>/*_test.go`). ~476 `*_test.go` files.
- **Conventions**: `testify` (assert + require), table-driven via `t.Run`. Hand-written narrow Service fakes (no gomock/mockery). `go-sqlmock` for DB mocking.
- **Heaviest packages**: `internal/application/service` (58), `internal/handler` (30), `internal/agent/tools` (24), `internal/types` (23).
- **Run**: `make test` (→ `go test -v ./...`) from repo root. Single: `go test -run TestFoo ./internal/format/`.
- **Must pass**: `go test -count=1 ./...` and `go vet ./...` before committing.

### Frontend Tests

- **Framework**: Node.js built-in test runner (`node --test`) — **not** vitest/jest/playwright.
- **Location**: Colocated `*.test.ts` siblings (12 files): `src/utils/`, `src/components/`, `src/views/knowledge/`, `src/composables/`.
- **Run**: `cd frontend && pnpm test`.

### CLI Acceptance Tests

- **Contract tests**: `cli/acceptance/contract/` — golden-pinned stdout JSON matrix. Sequential (no `t.Parallel` — iostreams singleton). `-update` flag refreshes goldens. Uses `httptest.Server` mock.
- **E2e tests**: `cli/acceptance/e2e/` — gated by `//go:build acceptance_e2e` tag. Full RAG loop: profile link → create KB → upload doc → search → chat.
- **Run contract**: `cd cli && go test ./acceptance/contract/...`
- **Run e2e**: `cd cli && WEKNORA_E2E_HOST=... WEKNORA_E2E_TOKEN=... go test -tags=acceptance_e2e -v ./acceptance/e2e/...`

### docreader Tests

- `cd docreader && pytest` — per-format parser tests, SSRF, concurrency, config.

### MCP Server Tests

- `cd mcp-server && pytest` — module, imports, file path security.

### Miniprogram Tests

- `cd miniprogram && npm test` → `node --test ../tests/miniprogram/*.test.js`.

### Linting

- **Backend**: `make lint` → `golangci-lint run` (config: `.golangci.yml`).
- **CLI**: `cd cli && make lint` → `go vet ./...`.
- **Frontend**: `pnpm run type-check` → `vue-tsc --build` (2 pre-existing errors in `KnowledgeBase.vue:665` are known).
