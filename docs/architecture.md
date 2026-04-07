# Architecture

## Overview

aigate is a multi-provider AI gateway written in Go. It translates OpenAI and Anthropic API calls into provider-specific formats, handling authentication, token refresh, streaming, and retry logic transparently.

```
Client (Cursor, Cline, SDK, curl...)
    │
    ▼
┌─────────────────────────────────────────┐
│         aigate (single binary)          │
│                                         │
│  ┌─────────────────────────────────┐    │
│  │         HTTP Router             │    │
│  │  /v1/chat/completions (OpenAI)  │    │
│  │  /v1/messages (Anthropic)       │    │
│  │  /v1/models (merged list)       │    │
│  └──────────┬──────────────────────┘    │
│             │                           │
│  ┌──────────▼──────────────────────┐    │
│  │     Provider Router             │    │
│  │  copilot/* → Copilot provider   │    │
│  │  *         → Kiro provider      │    │
│  └──────┬──────────────┬───────────┘    │
│         │              │                │
│  ┌──────▼─────┐ ┌──────▼──────┐        │
│  │   Kiro     │ │  Copilot    │        │
│  │  Provider  │ │  Provider   │        │
│  │            │ │             │        │
│  │ Auth:      │ │ Auth:       │        │
│  │ SSO OIDC / │ │ GitHub      │        │
│  │ Desktop /  │ │ OAuth +     │        │
│  │ SQLite     │ │ Token       │        │
│  │            │ │ Exchange    │        │
│  └──────┬─────┘ └──────┬──────┘        │
│         │              │                │
└─────────┼──────────────┼────────────────┘
          │              │
          ▼              ▼
   Kiro API         Copilot API
   (us-east-1)      (githubcopilot.com)
```

## Directory Structure

```
aigate/
├── cmd/aigate/main.go              # Entry point, CLI flags
├── internal/
│   ├── api/router.go               # HTTP handlers, provider routing
│   ├── config/config.go            # Environment config
│   ├── middleware/auth.go           # API key authentication
│   └── provider/
│       ├── provider.go             # Provider interface
│       ├── kiro/
│       │   ├── auth.go             # Token refresh, multi-source auth
│       │   ├── credentials.go      # Load from JSON/SQLite/env
│       │   ├── converter.go        # Message format conversion
│       │   ├── headers.go          # Kiro API headers
│       │   ├── provider.go         # Kiro provider implementation
│       │   └── stream.go           # SSE stream parser
│       └── copilot/
│           └── provider.go         # GitHub Copilot provider
├── docs/                           # Documentation
├── .github/workflows/release.yml   # Auto-build on tag
├── Dockerfile                      # Multi-stage, scratch image
├── Makefile                        # Cross-compile 5 platforms
└── README.md
```

## Provider Interface

All providers implement this interface:

```go
type Provider interface {
    Name() string
    ListModels(ctx context.Context) ([]ModelInfo, error)
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error)
}
```

## Request Flow

1. Client sends OpenAI or Anthropic format request
2. Router parses request, extracts model name
3. Provider router resolves model prefix → provider
4. Provider converts message format if needed
5. Provider authenticates (token refresh if expired)
6. Provider sends request to upstream API
7. Response streamed back through SSE or collected for non-streaming
8. Router formats response in OpenAI or Anthropic format

## Adding a New Provider

1. Create `internal/provider/<name>/provider.go`
2. Implement the `Provider` interface
3. Register in `internal/api/router.go` `NewRouterWithProvider()`
4. Add model prefix routing in `resolveProvider()`
