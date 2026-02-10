# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Antigravity Claude Proxy is a Go proxy server that exposes an Anthropic-compatible API backed by Antigravity's Cloud Code service. It enables using Claude models (`claude-sonnet-4-5-thinking`, `claude-opus-4-5-thinking`) and Gemini models (`gemini-3-flash`, `gemini-3-pro-low`, `gemini-3-pro-high`) with Claude Code CLI.

The proxy translates requests from Anthropic Messages API format → Google Generative AI format → Antigravity Cloud Code API, then converts responses back to Anthropic format with full thinking/streaming support.

**Tech Stack:** Go + Gin + Redis (replaced Node.js + Express + SQLite from upstream)

## Branch Structure

```
upstream/main (上游原版 Node.js)
    ↓ (git fetch & merge)
main (镜像分支，不做任何修改)
    ↓ (git merge main)
beta (Go 后端 + Redis，开发分支)
```

- **main**: Upstream mirror, never modify directly
- **beta**: Go backend implementation, active development
- Use `/sync-go-backend` skill to sync upstream changes

## Commands

```bash
# Frontend CSS build (requires Node.js)
npm install                  # Install frontend build deps + auto-build CSS
npm run build:css            # Build CSS once (minified)
npm run watch:css            # Watch CSS files for changes

# Go backend
go build ./...                     # Build
go vet ./...                       # Static analysis
go test ./...                      # Run tests

# Docker (local development)
docker-compose -f docker-compose.local.yml up -d

# Account management (via Go CLI)
go run cmd/accounts/main.go
```

## Architecture

**Request Flow:**
```
Claude Code CLI → Gin Server → CloudCode Client → Antigravity Cloud Code API
```

**Directory Structure:**

```
├── cmd/                            # Application entry points
│   ├── server/main.go              # Main server
│   ├── accounts/main.go            # Account management CLI
│   └── migrate/main.go             # Database migration
├── internal/                       # Core business logic
│   ├── server/                     # HTTP server & routing
│   ├── config/                     # Configuration & constants
│   │   ├── config.go               # Runtime configuration
│   │   ├── constants.go            # API endpoints, model mappings, enums
│   │   ├── server_presets.go       # Server configuration presets
│   │   └── presets.go              # Preset file management
│   ├── cloudcode/                  # Cloud Code API client
│   ├── account/                    # Multi-account pool management
│   │   └── strategies/             # Account selection strategies
│   ├── auth/                       # Authentication (OAuth, tokens)
│   ├── format/                     # Format conversion (Anthropic ↔ Google)
│   ├── webui/                      # WebUI backend API
│   │   └── handlers/               # API route handlers
│   ├── modules/                    # Feature modules (usage stats)
│   └── utils/                      # Utility functions
├── pkg/                            # Reusable packages
│   ├── anthropic/                  # Anthropic type definitions
│   └── redis/                      # Redis client wrapper
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
├── docker-compose.local.yml        # Local development compose
│
├── public/                         # Frontend (shared with upstream)
├── index.html                  # Main entry point
├── css/
│   ├── style.css               # Compiled Tailwind CSS (generated, do not edit)
│   └── src/
│       └── input.css           # Tailwind source with @apply directives
├── js/
│   ├── app.js                  # Main application logic (Alpine.js)
│   ├── config/constants.js     # Centralized UI constants
│   ├── store.js                # Global state management
│   ├── data-store.js           # Shared data store
│   ├── settings-store.js       # Settings management store
│   ├── components/             # UI Components
│   │   ├── dashboard.js        # Dashboard orchestrator
│   │   ├── account-manager.js  # Account list & OAuth
│   │   ├── models.js           # Model quota bars
│   │   ├── logs-viewer.js      # Live log streaming
│   │   ├── claude-config.js    # CLI settings editor
│   │   ├── server-config.js    # Server settings UI
│   │   └── dashboard/          # Dashboard sub-modules
│   ├── translations/           # i18n translation files
│   └── utils/                  # Frontend utilities
└── views/                      # HTML partials
```

**Key Modules (Go Backend):**

- **internal/server/**: Gin server exposing Anthropic-compatible endpoints (`/v1/messages`, `/v1/models`, `/health`, `/account-limits`)
- **internal/webui/**: WebUI backend handling API routes (`/api/*`)
- **internal/cloudcode/**: Cloud Code API client with retry/failover, streaming support
- **internal/account/**: Multi-account pool with configurable selection strategies
- **internal/auth/**: Google OAuth authentication
- **internal/format/**: Format conversion between Anthropic and Google Generative AI formats
- **internal/config/**: Configuration, constants, server presets
- **pkg/redis/**: Redis client (replaces SQLite from upstream)

**Node.js → Go Directory Mapping (for upstream sync):**

| Upstream Node.js (main branch) | Go Backend (beta branch) |
|-------------------------------|--------------------------|
| `src/server.js` | `internal/server/` |
| `src/constants.js` | `internal/config/constants.go` |
| `src/config.js` | `internal/config/config.go` |
| `src/cloudcode/*.js` | `internal/cloudcode/*.go` |
| `src/account-manager/*.js` | `internal/account/*.go` |
| `src/webui/index.js` | `internal/webui/*.go` |
| `src/auth/*.js` | `internal/auth/*.go` |
| `src/format/*.js` | `internal/format/*.go` |
| `src/utils/*.js` | `internal/utils/*.go` |
| `src/modules/*.js` | `internal/modules/*.go` |

**Multi-Account Load Balancing:**
- Configurable selection strategy via CLI flag or WebUI
- Three strategies: **Sticky** (cache-optimized), **Round-Robin** (load-balanced), **Hybrid** (smart, default)
- Account state persisted to Redis

**WebUI APIs:**

- `/api/accounts/*` - Account management (list, add, remove, refresh, threshold settings)
- `/api/config/*` - Server configuration (read/write)
- `/api/server/presets` - Server configuration presets (CRUD)
- `/api/strategy/health` - Strategy health data (gated behind devMode)
- `/api/claude/config` - Claude CLI settings
- `/api/logs/stream` - SSE endpoint for real-time logs
- `/api/stats/history` - 30-day request history
- `/account-limits` - Account quotas and subscription data

## Frontend Development

### CSS Build System

**Workflow:**
1. Edit styles in `public/css/src/input.css` (Tailwind source with `@apply` directives)
2. Run `npm run build:css` to compile (or `npm run watch:css` for auto-rebuild)
3. Compiled CSS output: `public/css/style.css` (minified, committed to git)

**When to rebuild:**
- After modifying `public/css/src/input.css`
- After pulling changes that updated CSS source
- Automatically on `npm install` (via `prepare` hook)

### Error Handling Pattern

Use `window.ErrorHandler.withLoading()` for async operations:

```javascript
async myOperation() {
  return await window.ErrorHandler.withLoading(async () => {
    const result = await someApiCall();
    if (!result.ok) throw new Error('Operation failed');
    return result;
  }, this, 'loading', { errorMessage: 'Failed to complete operation' });
}
```

### Account Operations Service Layer

Use `window.AccountActions` for account operations instead of direct API calls.

**Available methods:**
- `refreshAccount(email)` - Refresh token and quota
- `toggleAccount(email, enabled)` - Enable/disable account
- `deleteAccount(email)` - Delete account
- `getFixAccountUrl(email)` - Get OAuth re-auth URL
- `reloadAccounts()` - Reload from disk

All methods return `{success: boolean, data?: object, error?: string}`

## Web Management UI

- **Stack**: Vanilla JS + Alpine.js + Tailwind CSS + DaisyUI
- **i18n**: English, Chinese (中文), Indonesian (Bahasa), Portuguese (PT-BR), Turkish (Türkçe)
- **Security**: Optional password protection via `WEBUI_PASSWORD` env var

## Upstream Sync Workflow

Use the `/sync-go-backend` skill to synchronize upstream changes:

1. `git merge main` — frontend changes auto-merge
2. Check `git diff HEAD~1...HEAD --name-only -- src/` for backend changes
3. Read upstream Node.js code via `git show main:src/path/to/file.js`
4. Manually sync to corresponding Go files
5. Verify with `go build ./...` and `go vet ./...`

## Maintenance

When making significant changes to the codebase (new modules, refactoring, architectural changes), update this CLAUDE.md to keep documentation in sync.
