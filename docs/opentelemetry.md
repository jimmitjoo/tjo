# OpenTelemetry Integration

GemQuick includes built-in support for OpenTelemetry, enabling distributed tracing, metrics, and log correlation for production observability.

## Quick Start

### 1. Enable OpenTelemetry

Add these environment variables to your `.env` file:

```env
OTEL_ENABLED=true
OTEL_SERVICE_NAME=my-app
OTEL_SERVICE_VERSION=1.0.0
OTEL_ENDPOINT=localhost:4317
OTEL_INSECURE=true
```

### 2. Start a Collector (Jaeger)

```bash
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 4317:4317 \
  jaegertracing/all-in-one:latest
```

### 3. View Traces

Open http://localhost:16686 to see your traces in Jaeger UI.

---

## Configuration Reference

| Variable | Description | Default |
|----------|-------------|---------|
| `OTEL_ENABLED` | Enable OpenTelemetry | `false` |
| `OTEL_SERVICE_NAME` | Service name in traces | Required |
| `OTEL_SERVICE_VERSION` | Service version | Optional |
| `OTEL_ENVIRONMENT` | Deployment environment | Optional |
| `OTEL_EXPORTER` | Exporter type: `otlp`, `zipkin`, `none` | `otlp` |
| `OTEL_ENDPOINT` | Collector endpoint | Required |
| `OTEL_INSECURE` | Disable TLS | `false` |
| `OTEL_SAMPLER` | Sampling strategy: `always`, `never`, `ratio`, `parent` | `always` |
| `OTEL_SAMPLE_RATIO` | Sample ratio (0.0-1.0) when using `ratio` | `1.0` |
| `OTEL_ENABLE_METRICS` | Enable OTel metrics | `false` |

---

## Automatic Tracing

When enabled, GemQuick automatically traces:

- **HTTP Requests**: Every request gets a span with method, URL, status, duration
- **Response Headers**: `X-Trace-ID` header added to all responses

---

## Custom Instrumentation

### Creating Spans

```go
import "github.com/jimmitjoo/gemquick/otel"

func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
    // Create a span for this operation
    ctx, span := otel.Start(r.Context(), "process_order")
    defer span.End()

    // Add attributes
    otel.SetAttributes(ctx,
        otel.String("order.id", orderID),
        otel.Int("order.items", len(items)),
        otel.Float64("order.total", total),
    )

    // Add events
    otel.AddEvent(ctx, "validation_complete")

    // Process the order...
    if err := h.processPayment(ctx, order); err != nil {
        otel.RecordError(ctx, err)
        return
    }

    otel.AddEvent(ctx, "payment_processed")
}
```

### Using WithSpan Helper

```go
// Simple operations
err := otel.WithSpan(ctx, "validate_input", func(ctx context.Context) error {
    // Your code here
    return validateOrder(order)
})

// Operations that return a value
user, err := otel.WithSpanResult(ctx, "fetch_user", func(ctx context.Context) (*User, error) {
    return db.FindUser(ctx, userID)
})
```

### Span Options

```go
opts := otel.NewSpanOptions().
    WithKind(trace.SpanKindClient).
    WithAttribute("service", "payment").
    WithAttributes(
        otel.String("provider", "stripe"),
        otel.Bool("sandbox", true),
    )

ctx, span := otel.Start(ctx, "payment_request", opts.Build()...)
defer span.End()
```

---

## Database Tracing

### Wrapping the Database Connection

```go
import "github.com/jimmitjoo/gemquick/otel"

// Wrap your database connection
tracedDB := otel.WrapDB(db, "postgres", "myapp")

// All queries are now traced
rows, err := tracedDB.Query(ctx, "SELECT * FROM users WHERE active = ?", true)

// Transactions are traced as a single span
tx, err := tracedDB.Begin(ctx)
tx.Exec(ctx, "UPDATE users SET last_login = NOW() WHERE id = ?", userID)
tx.Exec(ctx, "INSERT INTO login_history (user_id) VALUES (?)", userID)
tx.Commit() // Span ends here
```

### Traced Operations

| Method | Span Name | Attributes |
|--------|-----------|------------|
| `Query` | `db.query` | db.system, db.statement |
| `QueryRow` | `db.query_row` | db.system, db.statement |
| `Exec` | `db.insert/update/delete` | db.system, db.statement, db.rows_affected |
| `Begin/Commit/Rollback` | `db.transaction` | db.transaction.status |
| `Prepare` | `db.prepare` | db.statement |

---

## Log Correlation

### Adding Trace Context to Logs

```go
import "github.com/jimmitjoo/gemquick/otel"

func (h *Handler) SomeHandler(w http.ResponseWriter, r *http.Request) {
    // Create a logger with trace context
    logger := otel.LoggerWithTrace(r.Context(), h.app.Logging.Logger)

    // All logs now include trace_id and span_id
    logger.Info("Processing request", map[string]interface{}{
        "user_id": userID,
    })
    // Output: {"trace_id":"abc123","span_id":"def456","user_id":"123",...}
}
```

### Using LogContext

```go
// Create a log context for the request
log := otel.NewLogContext(r.Context(), h.app.Logging.Logger)

log.Info("Starting process", nil)
log.Debug("Processing item", map[string]interface{}{"item_id": itemID})
log.Error("Failed to process", map[string]interface{}{"error": err.Error()})
```

### Manual Trace Fields

```go
// Add trace fields to any log call
h.app.Logging.Logger.Info("Event occurred", otel.MergeFields(ctx, map[string]interface{}{
    "custom_field": "value",
}))
```

---

## Extracting Trace Information

```go
// Get trace ID from context
traceID := otel.TraceIDFromContext(ctx)

// Get span ID from context
spanID := otel.SpanIDFromContext(ctx)

// Get full trace info
info := otel.GetTraceInfo(ctx)
fmt.Printf("Trace: %s, Span: %s, Sampled: %v\n",
    info.TraceID, info.SpanID, info.Sampled)
```

---

## Sampling Strategies

### Always Sample (Development)

```env
OTEL_SAMPLER=always
```

Every request is traced. Good for development and debugging.

### Ratio Sampling (Production)

```env
OTEL_SAMPLER=ratio
OTEL_SAMPLE_RATIO=0.1
```

Sample 10% of traces. Reduces overhead and costs in production.

### Parent-Based Sampling

```env
OTEL_SAMPLER=parent
```

Respects the sampling decision of the parent span. Good for distributed systems where you want consistent sampling across services.

### Never Sample (Disabled)

```env
OTEL_SAMPLER=never
```

Disables sampling completely. Spans are still created but not exported.

---

## Exporters

### OTLP (Recommended)

Works with any OpenTelemetry Collector, Jaeger, Grafana Tempo, etc.

```env
OTEL_EXPORTER=otlp
OTEL_ENDPOINT=localhost:4317
OTEL_INSECURE=true
```

### Zipkin

Direct export to Zipkin:

```env
OTEL_EXPORTER=zipkin
OTEL_ENDPOINT=http://localhost:9411/api/v2/spans
```

### None (Testing)

Disables export, but tracing still works internally:

```env
OTEL_EXPORTER=none
```

---

## Production Setup

### With Grafana Stack

```yaml
# docker-compose.yml
version: "3"
services:
  tempo:
    image: grafana/tempo:latest
    command: ["-config.file=/etc/tempo.yaml"]
    ports:
      - "4317:4317"   # OTLP gRPC
      - "3200:3200"   # Tempo query
    volumes:
      - ./tempo.yaml:/etc/tempo.yaml

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_AUTH_ANONYMOUS_ENABLED=true
```

### With Jaeger

```yaml
# docker-compose.yml
version: "3"
services:
  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "16686:16686"  # UI
      - "4317:4317"    # OTLP gRPC
```

---

## Semantic Conventions

GemQuick follows OpenTelemetry semantic conventions for attributes:

### HTTP Spans

| Attribute | Example |
|-----------|---------|
| `http.method` | `GET` |
| `http.url` | `/api/users` |
| `http.status_code` | `200` |
| `http.duration_ms` | `45.2` |
| `net.host.name` | `api.example.com` |

### Database Spans

| Attribute | Example |
|-----------|---------|
| `db.system` | `postgres` |
| `db.name` | `myapp` |
| `db.statement` | `SELECT * FROM users` |
| `db.operation` | `select` |
| `db.rows_affected` | `5` |

---

## Best Practices

1. **Use meaningful span names**: `process_order` not `handler`
2. **Add relevant attributes**: Include IDs, counts, and states
3. **Record errors**: Always use `otel.RecordError()` for failures
4. **Add events for milestones**: Mark important steps in processing
5. **Sample appropriately**: Use ratio sampling in production
6. **Correlate logs**: Always use `LoggerWithTrace()` for log correlation

---

## Troubleshooting

### No traces appearing

1. Check `OTEL_ENABLED=true`
2. Verify `OTEL_ENDPOINT` is correct
3. Check if collector is running: `curl localhost:4317`
4. Look for errors in application logs

### High latency

1. Use ratio sampling: `OTEL_SAMPLE_RATIO=0.1`
2. Check collector capacity
3. Consider async batching (enabled by default)

### Missing database traces

1. Use `otel.WrapDB()` to wrap your database
2. Pass context to all database methods
3. Verify database type is set correctly
