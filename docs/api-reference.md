# API Reference

## OpenAI-Compatible Endpoints

### POST /v1/chat/completions

Standard OpenAI Chat Completions API. Supports streaming and non-streaming.

**Headers:**
```
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

**Request body:**
```json
{
  "model": "claude-sonnet-4-5",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "stream": false,
  "max_tokens": 4096,
  "temperature": 0.7
}
```

**Non-streaming response:**
```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "model": "claude-sonnet-4-5",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "Hello! How can I help?"},
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 0,
    "completion_tokens": 0,
    "total_tokens": 0
  }
}
```

**Streaming response (SSE):**
```
data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","model":"claude-sonnet-4-5","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","model":"claude-sonnet-4-5","choices":[{"index":0,"delta":{"content":"Hello"}}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","model":"claude-sonnet-4-5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

### GET /v1/models

Returns merged model list from all active providers.

**Response:**
```json
{
  "object": "list",
  "data": [
    {"id": "claude-sonnet-4.5", "object": "model", "owned_by": "kiro"},
    {"id": "claude-haiku-4.5", "object": "model", "owned_by": "kiro"},
    {"id": "copilot/gpt-4.1", "object": "model", "owned_by": "copilot"}
  ]
}
```

---

## Anthropic-Compatible Endpoints

### POST /v1/messages

Standard Anthropic Messages API. Supports streaming and non-streaming.

**Headers:**
```
x-api-key: <API_KEY>
anthropic-version: 2023-06-01
Content-Type: application/json
```

**Request body:**
```json
{
  "model": "claude-sonnet-4-5",
  "max_tokens": 1024,
  "system": "You are a helpful assistant.",
  "messages": [
    {"role": "user", "content": "Hello!"}
  ],
  "stream": false
}
```

**Non-streaming response:**
```json
{
  "id": "msg_abc123",
  "type": "message",
  "role": "assistant",
  "model": "claude-sonnet-4-5",
  "content": [{"type": "text", "text": "Hello! How can I help?"}],
  "stop_reason": "stop"
}
```

---

## Utility Endpoints

### GET /health

```json
{"status": "ok", "version": "0.2.0", "time": "2026-04-07T03:00:00Z"}
```

### GET /

Same as `/health`.

---

## Authentication

All endpoints except `/` and `/health` require authentication.

**OpenAI style:**
```
Authorization: Bearer <API_KEY>
```

**Anthropic style:**
```
x-api-key: <API_KEY>
```

Both are supported on all endpoints.

---

## Model Routing

| Prefix | Provider | Example |
|--------|----------|---------|
| `copilot/` | GitHub Copilot | `copilot/gpt-4.1` |
| *(none)* | Kiro | `claude-sonnet-4-5` |

Model names are normalized automatically:
- `claude-sonnet-4-5` → `claude-sonnet-4.5`
- `claude-haiku-4-5` → `claude-haiku-4.5`
- `claude-sonnet-4-5-20250929` → `claude-sonnet-4.5` (version suffix stripped)
