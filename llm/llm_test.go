package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openaioption "github.com/openai/openai-go/v3/option"
)

// Every test here runs against an httptest server.
//
// No test may call a live third-party API: it needs a key, it costs money, it
// is slow, and it asserts nothing that a recorded response does not. Two tests
// in this repository once called real APIs and asserted only that something
// failed -- which was equally true with no network.

// The provider swap is one line, which is the whole promise of this package.
// The same Request, the same assertions, two providers.
func TestTheSameRequestWorksOnBothProviders(t *testing.T) {
	openaiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decode(t, r, &body)

		if body["model"] != "gpt-test" {
			t.Errorf("openai got model %v", body["model"])
		}
		// The system prompt is a message here.
		messages := body["messages"].([]any)
		first := messages[0].(map[string]any)
		if first["role"] != "system" || first["content"] != "be terse" {
			t.Errorf("openai system prompt = %v", first)
		}

		writeJSON(w, map[string]any{
			"id": "1", "model": "gpt-test", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index":         0,
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": "four"},
			}},
			"usage": map[string]any{"prompt_tokens": 11, "completion_tokens": 2, "total_tokens": 13},
		})
	}))
	defer openaiServer.Close()

	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decode(t, r, &body)

		if body["model"] != "claude-test" {
			t.Errorf("anthropic got model %v", body["model"])
		}
		// The system prompt is a top-level field here, not a message. Absorbing
		// that difference is what this package is for.
		system := body["system"].([]any)
		if system[0].(map[string]any)["text"] != "be terse" {
			t.Errorf("anthropic system prompt = %v", system)
		}
		if _, ok := body["max_tokens"]; !ok {
			t.Error("anthropic requires max_tokens and none was sent")
		}

		writeJSON(w, map[string]any{
			"id": "1", "model": "claude-test", "type": "message", "role": "assistant",
			"stop_reason": "end_turn",
			"content":     []any{map[string]any{"type": "text", "text": "four"}},
			"usage":       map[string]any{"input_tokens": 11, "output_tokens": 2},
		})
	}))
	defer anthropicServer.Close()

	req := Request{
		System:   "be terse",
		Messages: []Message{User("what is two plus two")},
	}

	for name, chat := range map[string]Chat{
		"openai":    OpenAI("test-key", openaioption.WithBaseURL(openaiServer.URL)),
		"anthropic": Anthropic("test-key", anthropicoption.WithBaseURL(anthropicServer.URL)),
	} {
		r := req
		if name == "openai" {
			r.Model = "gpt-test"
		} else {
			r.Model = "claude-test"
		}

		res, err := chat.Complete(context.Background(), r)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Text != "four" {
			t.Errorf("%s: text = %q", name, res.Text)
		}
		if res.Usage.InputTokens != 11 || res.Usage.OutputTokens != 2 {
			t.Errorf("%s: usage = %+v", name, res.Usage)
		}
	}
}

// Tool calls come back in one shape whichever provider produced them.
func TestToolCallsAreNormalised(t *testing.T) {
	openaiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decode(t, r, &body)

		tools := body["tools"].([]any)
		fn := tools[0].(map[string]any)["function"].(map[string]any)
		if fn["name"] != "lookup_order" {
			t.Errorf("openai tool = %v", fn)
		}

		writeJSON(w, map[string]any{
			"id": "1", "model": "gpt-test", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "tool_calls",
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []any{map[string]any{
						"id": "call_1", "type": "function",
						"function": map[string]any{"name": "lookup_order", "arguments": `{"id":"A-1"}`},
					}},
				},
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer openaiServer.Close()

	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decode(t, r, &body)

		tools := body["tools"].([]any)
		if tools[0].(map[string]any)["name"] != "lookup_order" {
			t.Errorf("anthropic tool = %v", tools[0])
		}

		writeJSON(w, map[string]any{
			"id": "1", "model": "claude-test", "type": "message", "role": "assistant",
			"stop_reason": "tool_use",
			"content": []any{map[string]any{
				"type": "tool_use", "id": "toolu_1", "name": "lookup_order",
				"input": map[string]any{"id": "A-1"},
			}},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer anthropicServer.Close()

	tool := ToolDef{
		Name:        "lookup_order",
		Description: "Look up an order",
		Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
	}

	for name, tc := range map[string]struct {
		chat  Chat
		model string
	}{
		"openai":    {OpenAI("k", openaioption.WithBaseURL(openaiServer.URL)), "gpt-test"},
		"anthropic": {Anthropic("k", anthropicoption.WithBaseURL(anthropicServer.URL)), "claude-test"},
	} {
		res, err := tc.chat.Complete(context.Background(), Request{
			Model:    tc.model,
			Messages: []Message{User("where is order A-1")},
			Tools:    []ToolDef{tool},
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(res.ToolCalls) != 1 {
			t.Fatalf("%s: %d tool calls", name, len(res.ToolCalls))
		}

		call := res.ToolCalls[0]
		if call.Name != "lookup_order" {
			t.Errorf("%s: name = %q", name, call.Name)
		}

		var args struct{ ID string }
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			t.Fatalf("%s: arguments: %v", name, err)
		}
		if args.ID != "A-1" {
			t.Errorf("%s: arguments = %s", name, call.Arguments)
		}
	}
}

// Anthropic has no response_format. Structured output is a forced tool call
// there, and this package does that translation so one Request works on both.
func TestStructuredOutputOnBothProviders(t *testing.T) {
	schema := &Schema{
		Name:       "sentiment",
		Definition: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"]}`),
		Strict:     true,
	}

	openaiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decode(t, r, &body)

		format := body["response_format"].(map[string]any)
		if format["type"] != "json_schema" {
			t.Errorf("openai response_format = %v", format)
		}

		writeJSON(w, map[string]any{
			"id": "1", "model": "gpt-test", "object": "chat.completion",
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": `{"label":"positive"}`}}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer openaiServer.Close()

	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decode(t, r, &body)

		// Forced into the one tool, which is how this is done here.
		choice := body["tool_choice"].(map[string]any)
		if choice["name"] != "sentiment" {
			t.Errorf("anthropic tool_choice = %v", choice)
		}

		writeJSON(w, map[string]any{
			"id": "1", "model": "claude-test", "type": "message", "role": "assistant",
			"stop_reason": "tool_use",
			"content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": "sentiment",
				"input": map[string]any{"label": "positive"}}},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer anthropicServer.Close()

	for name, tc := range map[string]struct {
		chat  Chat
		model string
	}{
		"openai":    {OpenAI("k", openaioption.WithBaseURL(openaiServer.URL)), "gpt-test"},
		"anthropic": {Anthropic("k", anthropicoption.WithBaseURL(anthropicServer.URL)), "claude-test"},
	} {
		res, err := tc.chat.Complete(context.Background(), Request{
			Model:    tc.model,
			Messages: []Message{User("how do I feel")},
			Schema:   schema,
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		var out struct{ Label string }
		if err := res.Into(&out); err != nil {
			t.Fatalf("%s: Into: %v (text %q)", name, err, res.Text)
		}
		if out.Label != "positive" {
			t.Errorf("%s: label = %q", name, out.Label)
		}
	}
}

func TestStreamingEmitsTextDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		for _, chunk := range []string{
			`{"id":"1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	chat := OpenAI("k", openaioption.WithBaseURL(server.URL))

	var deltas []string
	res, err := chat.Stream(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{User("hi")},
	}, func(d Delta) error {
		if d.Text != "" {
			deltas = append(deltas, d.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(deltas, "|") != "Hel|lo" {
		t.Errorf("deltas = %v, want the pieces as they arrived", deltas)
	}
	if res.Text != "Hello" {
		t.Errorf("assembled text = %q", res.Text)
	}
	if res.Usage.OutputTokens != 2 {
		t.Errorf("usage = %+v", res.Usage)
	}
}

// An error from the callback stops the stream. A caller writing to a closed
// connection needs to be able to give up.
func TestStreamStopsWhenTheCallbackFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := range 10 {
			fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"%d\"}}]}\n\n", i)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	stop := errors.New("client went away")
	seen := 0

	_, err := OpenAI("k", openaioption.WithBaseURL(server.URL)).
		Stream(context.Background(), Request{Model: "m", Messages: []Message{User("count")}},
			func(d Delta) error {
				seen++
				if seen == 3 {
					return stop
				}
				return nil
			})

	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the callback's error", err)
	}
	if seen != 3 {
		t.Fatalf("callback ran %d times after returning an error", seen)
	}
}

func TestEmbeddingsComeBackInInputOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately out of order, because the API returns an index and this
		// package places by it rather than trusting the ordering.
		writeJSON(w, map[string]any{
			"object": "list", "model": "embed-test",
			"data": []any{
				map[string]any{"object": "embedding", "index": 1, "embedding": []float64{0.3, 0.4}},
				map[string]any{"object": "embedding", "index": 0, "embedding": []float64{0.1, 0.2}},
			},
		})
	}))
	defer server.Close()

	vectors, err := OpenAIEmbedder("k", openaioption.WithBaseURL(server.URL)).
		Embed(context.Background(), "embed-test", []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}

	if len(vectors) != 2 {
		t.Fatalf("%d vectors", len(vectors))
	}
	if vectors[0][0] != 0.1 || vectors[1][0] != 0.3 {
		t.Fatalf("vectors are in the wrong order: %v", vectors)
	}
}

func TestRequestValidation(t *testing.T) {
	chat := OpenAI("k")

	for name, req := range map[string]Request{
		"no model":    {Messages: []Message{User("hi")}},
		"no messages": {Model: "m"},
		"tool result with no call id": {
			Model:    "m",
			Messages: []Message{{Role: RoleTool, Text: "result"}},
		},
	} {
		if _, err := chat.Complete(context.Background(), req); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A provider constructed without a key fails at call time, not at construction.
// An application that configures an optional provider it never uses should
// still start.
func TestNoAPIKeyFailsAtCallTime(t *testing.T) {
	for name, chat := range map[string]Chat{
		"openai":    OpenAI(""),
		"anthropic": Anthropic(""),
	} {
		_, err := chat.Complete(context.Background(), Request{
			Model: "m", Messages: []Message{User("hi")},
		})
		if !errors.Is(err, ErrNoAPIKey) {
			t.Errorf("%s: err = %v, want ErrNoAPIKey", name, err)
		}
	}
}

func decode(t *testing.T, r *http.Request, into any) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("decoding request: %v\n%s", err, body)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
