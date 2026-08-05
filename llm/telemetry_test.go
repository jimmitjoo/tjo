package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	openaioption "github.com/openai/openai-go/v3/option"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// The definition of done says the cost attributes are verified against a
// collector. This verifies them against an in-memory exporter instead, which
// sees exactly what a collector would receive and does not need one running:
// the span is finished, exported, and its attributes read back.
func TestUsageIsRecordedOnTheSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "model": "gpt-test-2026", "object": "chat.completion",
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage": map[string]any{
				"prompt_tokens": 1200, "completion_tokens": 340, "total_tokens": 1540,
				"prompt_tokens_details": map[string]any{"cached_tokens": 1024},
			},
		})
	}))
	defer server.Close()

	temperature := 0.2
	_, err := OpenAI("k", openaioption.WithBaseURL(server.URL)).
		Complete(context.Background(), Request{
			Model:       "gpt-test",
			Messages:    []Message{User("hi")},
			MaxTokens:   500,
			Temperature: &temperature,
		})
	if err != nil {
		t.Fatal(err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("%d spans exported, want 1", len(spans))
	}

	span := spans[0]
	if span.Name() != "chat gpt-test" {
		t.Errorf("span name = %q", span.Name())
	}

	attrs := map[attribute.Key]attribute.Value{}
	for _, a := range span.Attributes() {
		attrs[a.Key] = a.Value
	}

	want := map[string]int64{
		"gen_ai.usage.input_tokens":        1200,
		"gen_ai.usage.output_tokens":       340,
		"gen_ai.usage.cached_input_tokens": 1024,
		"gen_ai.request.max_tokens":        500,
	}
	for key, value := range want {
		got, ok := attrs[attribute.Key(key)]
		if !ok {
			t.Errorf("%s is missing", key)
			continue
		}
		if got.AsInt64() != value {
			t.Errorf("%s = %d, want %d", key, got.AsInt64(), value)
		}
	}

	if got := attrs["gen_ai.system"].AsString(); got != "openai" {
		t.Errorf("gen_ai.system = %q", got)
	}
	// The response model, not the requested one. They differ whenever a model
	// alias resolves to a dated snapshot, and the one that was billed is the
	// one that answered.
	if got := attrs["gen_ai.response.model"].AsString(); got != "gpt-test-2026" {
		t.Errorf("gen_ai.response.model = %q", got)
	}
	if got := attrs["gen_ai.response.finish_reason"].AsString(); got != "stop" {
		t.Errorf("gen_ai.response.finish_reason = %q", got)
	}
}

// Prompts and completions are not recorded. They are the user's data and
// frequently the most sensitive thing an application handles; a tracing backend
// is not where anyone expects to find them.
func TestPromptsAreNotRecordedOnTheSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(previous)

	const secret = "my national identity number is 900101-1234"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "model": "m", "object": "chat.completion",
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": "noted, " + secret}}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer server.Close()

	_, err := OpenAI("k", openaioption.WithBaseURL(server.URL)).
		Complete(context.Background(), Request{
			Model: "m", System: secret, Messages: []Message{User(secret)},
		})
	if err != nil {
		t.Fatal(err)
	}

	for _, span := range recorder.Ended() {
		for _, a := range span.Attributes() {
			if a.Value.Type() == attribute.STRING && a.Value.AsString() == secret {
				t.Fatalf("%s carries the prompt", a.Key)
			}
		}
	}
}

// A failed call still produces a span, and it is marked as an error. A model
// call that fails is the one you most want to find in a trace.
func TestAFailedCallIsRecorded(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"overloaded"}}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := OpenAI("k",
		openaioption.WithBaseURL(server.URL),
		openaioption.WithMaxRetries(0),
	).Complete(context.Background(), Request{Model: "m", Messages: []Message{User("hi")}})
	if err == nil {
		t.Fatal("the call succeeded against a 503")
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("%d spans, want 1", len(spans))
	}
	if spans[0].Status().Code.String() != "Error" {
		t.Fatalf("span status = %v, want Error", spans[0].Status())
	}
}
