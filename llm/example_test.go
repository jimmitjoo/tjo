package llm_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/jimmitjoo/tjo/llm"
	"github.com/jimmitjoo/tjo/sse"
	openaioption "github.com/openai/openai-go/v3/option"
)

// Streaming a model's answer straight to a browser.
//
// This is the composition the two packages were shaped for: llm.Stream calls
// back with each delta, sse.Stream writes it to the client, and neither package
// knows about the other. SSE is the transport every major LLM API already
// streams over, which is why the framework implements the wire format rather
// than integrating with a client library.
func Example_streamingToABrowser() {
	// Stands in for the provider. A real application passes no base URL.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		for _, word := range []string{"Durable", "steps", "resume."} {
			chunk, _ := json.Marshal(map[string]any{
				"id": "1", "object": "chat.completion.chunk", "model": "gpt-test",
				"choices": []any{map[string]any{
					"index": 0,
					"delta": map[string]any{"content": word},
				}},
			})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer provider.Close()

	chat := llm.OpenAI("api-key", openaioption.WithBaseURL(provider.URL))

	// The handler: one stream in, one stream out.
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := sse.New(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = chat.Stream(r.Context(), llm.Request{
			Model:    "gpt-test",
			Messages: []llm.Message{llm.User("explain durable steps")},
		}, func(delta llm.Delta) error {
			if delta.Text == "" {
				return nil
			}
			// Send returns the request context's error once the client has
			// gone away, so returning it here stops the model call too --
			// which is the difference between a closed tab and a bill.
			return stream.Patch("token", delta.Text)
		})
		if err != nil {
			return
		}
	}))
	defer app.Close()

	response, err := http.Get(app.URL)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer response.Body.Close()

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if line := scanner.Text(); strings.HasPrefix(line, "data: ") {
			fmt.Println(strings.TrimPrefix(line, "data: "))
		}
	}

	// Output:
	// Durable
	// steps
	// resume.
}

// A tool call, answered, and handed back for the model to use.
//
// The loop is the caller's, deliberately: how many rounds to allow, which tools
// to expose and what to do when the model asks for one that does not exist are
// application decisions, and a framework that owned the loop would be an agent
// framework.
func Example_toolCalling() {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "application/json")

		// First call: ask for the tool. Second: answer with its result.
		if len(body.Messages) == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"id": "1", "model": "m", "object": "chat.completion",
				"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls",
					"message": map[string]any{"role": "assistant", "tool_calls": []any{
						map[string]any{"id": "call_1", "type": "function",
							"function": map[string]any{"name": "order_status", "arguments": `{"id":"A-1"}`}},
					}}}},
				"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id": "2", "model": "m", "object": "chat.completion",
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": "Order A-1 shipped on Tuesday."}}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer provider.Close()

	chat := llm.OpenAI("api-key", openaioption.WithBaseURL(provider.URL))
	ctx := context.Background()

	messages := []llm.Message{llm.User("where is order A-1?")}
	tools := []llm.ToolDef{{
		Name:        "order_status",
		Description: "Look up an order's status",
		Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
	}}

	res, err := chat.Complete(ctx, llm.Request{Model: "m", Messages: messages, Tools: tools})
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, call := range res.ToolCalls {
		// Whatever the application's own function returns.
		result := `{"status":"shipped","date":"Tuesday"}`

		messages = append(messages,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
			llm.Tool(call.ID, result),
		)
	}

	res, err = chat.Complete(ctx, llm.Request{Model: "m", Messages: messages, Tools: tools})
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(res.Text)

	// Output:
	// Order A-1 shipped on Tuesday.
}
