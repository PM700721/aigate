# Contributing

## Development Setup

```bash
git clone https://github.com/hoazgazh/aigate.git
cd aigate
go mod download
go build ./...
```

Requirements:
- Go 1.22+
- No CGO required (pure Go SQLite)

## Running locally

```bash
API_KEY=test go run ./cmd/aigate
```

## Project Structure

```
cmd/aigate/         → Entry point
internal/api/       → HTTP handlers
internal/config/    → Configuration
internal/middleware/ → Auth middleware
internal/provider/  → Provider interface + implementations
  ├── provider.go   → Interface definition
  ├── kiro/         → Kiro provider
  └── copilot/      → GitHub Copilot provider
```

## Adding a New Provider

1. Create `internal/provider/<name>/provider.go`
2. Implement the `Provider` interface:

```go
type Provider interface {
    Name() string
    ListModels(ctx context.Context) ([]ModelInfo, error)
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error)
}
```

3. Register in `internal/api/router.go`:

```go
myProvider, err := myprovider.New()
if err == nil {
    s.providers["myprovider"] = myProvider
}
```

4. Add routing in `resolveProvider()`:

```go
if strings.HasPrefix(model, "myprovider/") {
    if p, ok := s.providers["myprovider"]; ok {
        return p, strings.TrimPrefix(model, "myprovider/")
    }
}
```

## Building Releases

```bash
# Single platform
make build

# All platforms
make all

# Output in bin/
```

## Cross-compile targets

| Binary | OS | Arch |
|--------|-----|------|
| `aigate-darwin-amd64` | macOS | Intel |
| `aigate-darwin-arm64` | macOS | Apple Silicon |
| `aigate-linux-amd64` | Linux | x86_64 |
| `aigate-linux-arm64` | Linux | ARM64 |
| `aigate-windows-amd64.exe` | Windows | x86_64 |

## Code Style

- Standard Go formatting (`gofmt`)
- No external web frameworks (stdlib `net/http`)
- Errors returned explicitly, no panics
- Logging via `log` stdlib package
