# Tjo Modules

Tjo uses opt-in modules for SMS, Email, WebSocket, OpenTelemetry and LLMs.
Import only what you need.

Each is a **separate Go module** with its own `go.mod`, tagged in lockstep with
the framework. The rule for what becomes a module is dependency weight: a module
exists when it would otherwise put a vendor SDK in the graph of every
application that does not use it.

## Quick Start

```go
import (
    "github.com/jimmitjoo/tjo"
    "github.com/jimmitjoo/tjo/sms"
    "github.com/jimmitjoo/tjo/email"
    "github.com/jimmitjoo/tjo/websocket"
    "github.com/jimmitjoo/tjo/otel"
    "github.com/jimmitjoo/tjo/llm"
)

func main() {
    app := tjo.Tjo{}
    app.New(rootPath,
        sms.NewModule(),
        email.NewModule(),
        websocket.NewModule(),
        otel.NewModule(),
    )
}
```

---

## SMS Module

### Built-in Providers

| Provider | Package |
|----------|---------|
| Twilio | `sms.WithTwilio()` |
| Vonage | `sms.WithVonage()` |

### Environment Variables

```env
# Auto-detected provider
SMS_PROVIDER=twilio  # or "vonage"

# Twilio
TWILIO_ACCOUNT_SID=your_account_sid
TWILIO_API_KEY=your_api_key
TWILIO_API_SECRET=your_api_secret
TWILIO_FROM_NUMBER=+1234567890

# Vonage
VONAGE_API_KEY=your_api_key
VONAGE_API_SECRET=your_api_secret
VONAGE_FROM_NUMBER=+1234567890
```

### Usage

```go
// Option 1: From environment
app.New(rootPath, sms.NewModule())

// Option 2: Explicit configuration
app.New(rootPath, sms.NewModule(
    sms.WithTwilio("account_sid", "api_key", "api_secret", "+1234567890"),
))

// Option 3: Vonage
app.New(rootPath, sms.NewModule(
    sms.WithVonage("api_key", "api_secret", "+1234567890"),
))

// Send SMS
if m := app.GetModule("sms"); m != nil {
    smsModule := m.(*sms.Module)
    err := smsModule.Send("+46701234567", "Hello from Tjo!", false)
}
```

### Custom SMS Provider

```go
package myprovider

type MyProvider struct {
    APIKey string
}

func (p *MyProvider) Send(to, message string, unicode bool) error {
    // Your implementation
    return nil
}

// Use it
app.New(rootPath, sms.NewModule())
if m := app.GetModule("sms"); m != nil {
    m.(*sms.Module).Provider = &myprovider.MyProvider{APIKey: "..."}
}
```

---

## Email Module

### Built-in Methods

| Method | Description |
|--------|-------------|
| SMTP | Traditional SMTP sending |
| API | Mailgun, SendGrid, SparkPost |

### Environment Variables

```env
# SMTP Configuration
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=your_username
SMTP_PASSWORD=your_password
SMTP_ENCRYPTION=tls  # tls, ssl, or none

# Sender defaults
MAIL_FROM_ADDRESS=noreply@example.com
MAIL_FROM_NAME=My App
MAIL_DOMAIN=example.com

# API Provider (alternative to SMTP)
MAILER_API=mailgun  # mailgun, sendgrid, sparkpost
MAILER_KEY=your_api_key
MAILER_URL=https://api.mailgun.net/v3

# Templates directory
EMAIL_TEMPLATES=./email
```

### Usage

```go
// Option 1: From environment
app.New(rootPath, email.NewModule())

// Option 2: Explicit SMTP
app.New(rootPath, email.NewModule(
    email.WithSMTP("smtp.gmail.com", 587, "user@gmail.com", "password", "tls"),
    email.WithFrom("noreply@example.com", "My App"),
    email.WithTemplates("./email"),
))

// Option 3: API Provider
app.New(rootPath, email.NewModule(
    email.WithAPI("mailgun", "api_key", "https://api.mailgun.net/v3", "example.com"),
))

// Send email
if m := app.GetModule("email"); m != nil {
    emailModule := m.(*email.Module)

    // Simple send
    err := emailModule.Send(email.Message{
        To:      "user@example.com",
        Subject: "Welcome!",
        Data:    map[string]interface{}{"name": "John"},
        Template: "welcome",  // Uses ./email/welcome.html.tmpl
    })

    // Async send (queued)
    emailModule.Queue(email.Message{
        To:      "user@example.com",
        Subject: "Newsletter",
    })
}
```

### Email Templates

Create templates in your email directory:

**email/welcome.html.tmpl:**
```html
<!DOCTYPE html>
<html>
<body>
    <h1>Welcome, {{.name}}!</h1>
    <p>Thanks for signing up.</p>
</body>
</html>
```

**email/welcome.plain.tmpl:**
```
Welcome, {{.name}}!

Thanks for signing up.
```

---

## WebSocket Module

### Environment Variables

No environment variables required. Configure via options.

### Usage

```go
// Basic setup
app.New(rootPath, websocket.NewModule())

// With options
app.New(rootPath, websocket.NewModule(
    websocket.WithAllowedOrigins([]string{"https://example.com"}),
    websocket.WithReadBufferSize(1024),
    websocket.WithWriteBufferSize(1024),
    websocket.WithAuthenticateConnection(func(r *http.Request) (string, error) {
        token := r.Header.Get("Authorization")
        userID, err := validateToken(token)
        return userID, err
    }),
))

// Mount handler
if m := app.GetModule("websocket"); m != nil {
    wsModule := m.(*websocket.Module)
    app.HTTP.Router.Get("/ws", wsModule.Handler())
}

// Broadcast to all clients
if m := app.GetModule("websocket"); m != nil {
    m.(*websocket.Module).Broadcast([]byte(`{"type":"update"}`))
}

// Broadcast to room
if m := app.GetModule("websocket"); m != nil {
    m.(*websocket.Module).BroadcastToRoom("chat:123", []byte(`{"msg":"hello"}`), nil)
}

// Get client count
if m := app.GetModule("websocket"); m != nil {
    count := m.(*websocket.Module).GetClientCount()
}
```

### Client-side JavaScript

```javascript
const ws = new WebSocket('wss://example.com/ws');

ws.onopen = () => {
    // Join a room
    ws.send(JSON.stringify({action: 'join', room: 'chat:123'}));
};

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log('Received:', data);
};

// Send message to room
ws.send(JSON.stringify({
    action: 'message',
    room: 'chat:123',
    data: {text: 'Hello!'}
}));

// Leave room
ws.send(JSON.stringify({action: 'leave', room: 'chat:123'}));
```

---

## OpenTelemetry Module

### Environment Variables

```env
OTEL_ENABLED=true
OTEL_SERVICE_NAME=my-app
OTEL_SERVICE_VERSION=1.0.0
OTEL_ENVIRONMENT=production

# Exporter
OTEL_EXPORTER=otlp  # otlp, zipkin, none
OTEL_ENDPOINT=localhost:4317
OTEL_INSECURE=true  # Set false for production

# Sampling
OTEL_SAMPLER=always  # always, never, ratio, parent
OTEL_SAMPLE_RATIO=0.1  # When using ratio sampler

# Metrics (optional)
OTEL_METRICS_ENABLED=true
```

### Usage

```go
// Option 1: From environment
app.New(rootPath, otel.NewModule())

// Option 2: Explicit configuration
app.New(rootPath, otel.NewModule(
    otel.WithServiceName("my-app"),
    otel.WithServiceVersion("1.0.0"),
    otel.WithEnvironment("production"),
    otel.WithOTLPExporter("localhost:4317", true),
    otel.WithRatioSample(0.1),
))

// Option 3: Zipkin exporter
app.New(rootPath, otel.NewModule(
    otel.WithServiceName("my-app"),
    otel.WithZipkinExporter("http://localhost:9411/api/v2/spans"),
))
```

### Creating Spans

```go
import "github.com/jimmitjoo/tjo/otel"

func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
    // Start a span
    ctx, span := otel.Start(r.Context(), "process_order")
    defer span.End()

    // Add attributes
    otel.SetAttributes(ctx,
        otel.String("order.id", orderID),
        otel.Int("order.items", len(items)),
    )

    // Record errors
    if err != nil {
        otel.RecordError(ctx, err)
    }

    // Create child spans
    ctx2, span2 := otel.Start(ctx, "validate_payment")
    defer span2.End()
}
```

### Database Tracing

```go
// Wrap your database connection
tracedDB := otel.WrapDB(db, "postgres", "mydb")

// All queries are automatically traced
rows, err := tracedDB.QueryContext(ctx, "SELECT * FROM users WHERE id = $1", userID)
```

### Local Development

```bash
# Start Jaeger for local trace viewing
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 4317:4317 \
  jaegertracing/all-in-one:latest

# View traces at http://localhost:16686
```

---

## Creating Custom Modules

Implement the `Module` interface:

```go
package mymodule

import (
    "context"
    "github.com/jimmitjoo/tjo"
)

type Module struct {
    config Config
}

type Config struct {
    APIKey string
}

type Option func(*Module)

func NewModule(opts ...Option) *Module {
    m := &Module{}
    for _, opt := range opts {
        opt(m)
    }
    return m
}

func WithAPIKey(key string) Option {
    return func(m *Module) {
        m.config.APIKey = key
    }
}

func (m *Module) Name() string {
    return "mymodule"
}

func (m *Module) Initialize(app any) error {
    // Setup your module
    // g is *tjo.Tjo but typed as interface{} for decoupling
    return nil
}

func (m *Module) Shutdown(ctx context.Context) error {
    // Cleanup
    return nil
}
```

**Usage:**
```go
app.New(rootPath,
    mymodule.NewModule(mymodule.WithAPIKey("secret")),
)
```

---

## Module Lifecycle

1. **Registration**: Modules are registered during `app.New()`
2. **Initialization**: `Initialize()` is called in registration order
3. **Runtime**: Access via `app.GetModule("name")`
4. **Shutdown**: `Shutdown()` is called in reverse order during `app.Shutdown()`

```go
// Graceful shutdown
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
app.Shutdown(ctx)
```

---

## LLM Module

A thin layer over the first-party Go SDKs: chat with streaming, tool calling,
structured output, and embeddings. Thin is the specification — evals, prompt
management, chunking and agent orchestration are products, not framework
features.

Its own module because the 2025 Go Developer Survey found 22% of Go developers
building AI features. The other 78% should not carry two vendor SDKs to get a
router.

### Swapping providers is one line

```go
chat := llm.OpenAI(os.Getenv("OPENAI_API_KEY"))
chat := llm.Anthropic(os.Getenv("ANTHROPIC_API_KEY"))
```

Everything after that line is the same. The differences between the two APIs are
absorbed rather than surfaced: the system prompt is a message on one and a field
on the other, `max_tokens` is optional on one and required on the other, a tool
result is its own role on one and a user message on the other, and structured
output is `response_format` on one and a forced tool call on the other.

### Chat

```go
res, err := chat.Complete(ctx, llm.Request{
    Model:    "gpt-5",
    System:   "Answer in one sentence.",
    Messages: []llm.Message{llm.User("Why is the sky blue?")},
})
fmt.Println(res.Text, res.Usage.InputTokens, res.Usage.OutputTokens)
```

### Streaming, into an SSE response

```go
stream, _ := sse.New(w, r)

_, err := chat.Stream(ctx, req, func(delta llm.Delta) error {
    if delta.Text == "" {
        return nil
    }
    return stream.Patch("token", delta.Text)
})
```

`Stream` returns the request context's error once the client has gone away, so
returning it from the callback stops the model call too — the difference between
a closed tab and a bill.

### Tool calling

```go
res, _ := chat.Complete(ctx, llm.Request{
    Model:    model,
    Messages: messages,
    Tools: []llm.ToolDef{{
        Name:        "order_status",
        Description: "Look up an order's status",
        Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
    }},
})

for _, call := range res.ToolCalls {
    result := lookUp(call.Arguments)
    messages = append(messages,
        llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
        llm.Tool(call.ID, result),
    )
}
```

The loop is yours. How many rounds to allow and what to do when the model asks
for a tool that does not exist are application decisions; a framework that owned
the loop would be an agent framework.

### Structured output

```go
res, _ := chat.Complete(ctx, llm.Request{
    Model:    model,
    Messages: messages,
    Schema: &llm.Schema{
        Name:       "sentiment",
        Definition: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}}}`),
        Strict:     true,
    },
})

var out struct{ Label string }
err := res.Into(&out)
```

### Embeddings and vector search

```go
vectors, _ := llm.OpenAIEmbedder(key).Embed(ctx, "text-embedding-3-small", []string{doc})

rows, _ := database.NewQueryBuilder(db).WithDialect(database.DialectDollar).
    Table("documents").
    Where("organization_id", "=", orgID).
    Nearest("embedding", vectors[0], database.Cosine).
    Limit(10).
    Get()
```

Vector search is in the root module's query builder, not here: it needs no SDK,
and the reason to keep the vector in the primary database is that the tenancy
filter and the similarity ordering compose into one statement.

### Cost accounting

Token counts are emitted as `gen_ai.*` OpenTelemetry span attributes —
`input_tokens`, `output_tokens` and `cached_input_tokens` — on the span the call
already produces. Grouping, summing and alerting on them belongs in the
observability backend, so that is where they are put and nothing more is done.

Prompts and completions are deliberately **not** recorded. They are the user's
data and a tracing backend is not where anyone expects to find them.

### Environment Variables

```env
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
```

A provider constructed without a key fails at call time, not at construction, so
an application that configures a provider it never uses still starts.
