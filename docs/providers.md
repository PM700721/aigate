# Providers

## Kiro

Kiro provides free access to Claude models through Amazon Q Developer / AWS CodeWhisperer infrastructure.

### Models

| Model | Description |
|-------|-------------|
| `claude-sonnet-4-5` | Claude Sonnet 4.5 — balanced performance |
| `claude-haiku-4-5` | Claude Haiku 4.5 — fast responses |
| `claude-sonnet-4` | Claude Sonnet 4 — previous gen |
| `claude-3.7-sonnet` | Claude 3.7 Sonnet — legacy |
| `deepseek-v3.2` | DeepSeek V3.2 — open MoE model |
| `minimax-m2.1` | MiniMax M2.1 — planning & workflows |
| `qwen3-coder-next` | Qwen3 Coder — coding focused |

### Authentication

Kiro supports 4 credential sources (checked in priority order):

1. **`KIRO_CLI_DB_FILE`** — SQLite database from kiro-cli
2. **`KIRO_CREDS_FILE`** — JSON file from Kiro IDE
3. **`REFRESH_TOKEN`** — Direct refresh token
4. **Auto-detect** — Checks common paths automatically

#### Auto-detect paths

| Path | Source |
|------|--------|
| `~/.local/share/kiro-cli/data.sqlite3` | kiro-cli (Linux/WSL) |
| `~/.local/share/amazon-q/data.sqlite3` | amazon-q-developer-cli |
| `~/.aws/sso/cache/kiro-auth-token.json` | Kiro IDE (macOS) |

#### Auth types

aigate auto-detects the auth type from credentials:

- **Kiro Desktop** — Uses `https://prod.{region}.auth.desktop.kiro.dev/refreshToken`
- **AWS SSO OIDC** — Uses `https://oidc.{region}.amazonaws.com/token` (when `clientId`/`clientSecret` present)
- **Enterprise** — Loads device registration from `~/.aws/sso/cache/{clientIdHash}.json`

### Token Refresh

- Tokens auto-refresh 10 minutes before expiry
- Updated tokens are saved back to the original source (JSON file or SQLite)
- For SQLite mode: reloads from DB first (kiro-cli may have refreshed)
- Graceful degradation: if refresh fails but token not yet expired, continues using it

### API Details

- Endpoint: `https://q.us-east-1.amazonaws.com/generateAssistantResponse`
- API region is always `us-east-1` regardless of SSO region
- Retry logic: 403 (token refresh) → 429 (backoff) → 5xx (backoff)

---

## GitHub Copilot

GitHub Copilot Free provides 2000 code completions and 50 premium requests per month.

### Models

Use `copilot/` prefix to route to Copilot:

| Model | Description |
|-------|-------------|
| `copilot/gpt-4.1` | GPT-4.1 |
| `copilot/claude-3.5-sonnet` | Claude 3.5 Sonnet |
| `copilot/o4-mini` | OpenAI o4-mini |

Available models depend on your Copilot subscription tier.

### Authentication

1. Run `./aigate --copilot-login`
2. Open the URL shown in terminal
3. Enter the device code in your browser
4. Authorize the GitHub OAuth app
5. Token saved to `~/.local/share/aigate/github_token`

#### Token flow

```
GitHub OAuth device code flow
    → GitHub access token (persisted)
        → Exchange for Copilot token (short-lived, auto-refreshed)
            → API calls to api.githubcopilot.com
```

### API Details

- Token exchange: `https://api.github.com/copilot_internal/v2/token`
- Chat endpoint: `https://api.githubcopilot.com/chat/completions`
- Models endpoint: `https://api.githubcopilot.com/models`
- Copilot tokens expire every ~30 minutes, auto-refreshed

### Limitations

- Free tier: 2000 completions/month, 50 premium requests/month
- Pro ($10/mo): Unlimited completions, 300 premium requests/month
- Rate limits enforced by GitHub
