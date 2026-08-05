// Package llm is a thin, provider-agnostic layer over the first-party Go SDKs:
// chat with streaming, tool calling, structured output, and embeddings.
//
// Thin is the whole specification. What this package contains is the four
// things that have stabilised across providers and nothing else.
//
// # What is deliberately absent
//
// Evals, prompt management, RAG chunking strategy, agent orchestration. Those
// are products -- Langfuse and promptfoo are each a company's whole output --
// and a framework that ships an opinion about chunking will be wrong within a
// year. Cost *control* is absent for the same reason: the pattern has not
// converged, so this emits the numbers as span attributes and stops.
//
// # Why build on the first-party SDKs
//
// langchaingo is the one everyone reaches for first and had not been pushed in
// seven months when this was written. There is no Go equivalent of the Vercel
// AI SDK and that gap is not one framework's to fill. `openai/openai-go` and
// `anthropics/anthropic-sdk-go` are both actively maintained by the people
// whose API it is, which makes them the stable thing to be thin over.
//
// # It is opt-in, and that is a measurement rather than a preference
//
// The 2025 Go Developer Survey found 22% of Go developers building AI-powered
// features. This is a separate module for the same reason `sms` and `email`
// are: 78% of applications should not carry two vendor SDKs in their
// dependency graph to get a router.
//
// # Swapping providers is one line
//
//	chat := llm.OpenAI(os.Getenv("OPENAI_API_KEY"))
//	chat := llm.Anthropic(os.Getenv("ANTHROPIC_API_KEY"))
//
// Everything below that line is the same, which is what "provider-agnostic"
// has to mean to be worth anything.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Role is who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one turn.
//
// The system prompt is not a message here. Providers disagree about whether it
// is a message, a top-level field or a list, and a caller that has to know
// which is not using a provider-agnostic API. It is Request.System.
type Message struct {
	Role Role

	// Text is the content.
	Text string

	// ToolCalls are the calls an assistant message asked for.
	ToolCalls []ToolCall

	// ToolCallID identifies which call a tool message answers. Required on a
	// tool message and ignored elsewhere.
	ToolCallID string
}

// User, Assistant and Tool build the three messages a caller writes by hand.
func User(text string) Message      { return Message{Role: RoleUser, Text: text} }
func Assistant(text string) Message { return Message{Role: RoleAssistant, Text: text} }

// Tool answers a tool call. The result is whatever the caller's function
// returned, as text -- JSON if the model should parse it.
func Tool(callID, result string) Message {
	return Message{Role: RoleTool, ToolCallID: callID, Text: result}
}

// ToolDef is a function the model may call.
//
// Schema is JSON Schema, as bytes. Not a struct: every provider takes JSON
// Schema, the schema is usually written once as a literal, and a Go type
// mirroring the specification would be a second thing to keep current with it.
type ToolDef struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// ToolCall is the model asking for a function to run.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Request is one call to a model.
type Request struct {
	// Model is the provider's model id. There is no cross-provider naming
	// here: inventing one would mean maintaining a mapping that is wrong every
	// time either provider ships a model.
	Model string

	// System is the system prompt.
	System string

	Messages []Message

	// Tools the model may call.
	Tools []ToolDef

	// Schema makes the model answer with JSON matching it. The text of the
	// response is then that JSON.
	Schema *Schema

	// MaxTokens bounds the response. Zero means the provider's default, except
	// on Anthropic, where the API requires a value and DefaultMaxTokens is
	// used.
	MaxTokens int

	// Temperature is passed through when set.
	Temperature *float64
}

// DefaultMaxTokens is used when a provider requires a limit and the caller did
// not give one.
const DefaultMaxTokens = 4096

// Schema describes the shape of a structured response.
type Schema struct {
	Name        string
	Description string
	Definition  json.RawMessage

	// Strict asks the provider to guarantee conformance where it can.
	Strict bool
}

// Response is what came back.
type Response struct {
	Text      string
	ToolCalls []ToolCall

	// StopReason is the provider's own value, passed through rather than
	// mapped. A normalised enum would be a lie the first time a provider adds
	// a reason.
	StopReason string

	Model string
	Usage Usage
}

// Into unmarshals a structured response into v.
func (r *Response) Into(v any) error {
	if r.Text == "" {
		return errors.New("llm: the response has no text to decode")
	}
	return json.Unmarshal([]byte(r.Text), v)
}

// Usage is what the call cost, in tokens.
type Usage struct {
	InputTokens  int
	OutputTokens int

	// CachedInputTokens is the part of the input that was served from the
	// provider's prompt cache. Both providers report it and it is the
	// difference between a cheap call and an expensive one, so it is not
	// folded into InputTokens.
	CachedInputTokens int
}

// Total is every token the call touched.
func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }

// Delta is one piece of a streamed response.
type Delta struct {
	// Text is the new text, not the accumulated text.
	Text string

	// ToolCall is set when a tool call has finished arriving. Partial tool
	// calls are not emitted: a half-parsed JSON argument list is not something
	// a caller can act on, and buffering it here means every caller does not
	// have to.
	ToolCall *ToolCall
}

// Chat is a model that answers.
//
// Two methods. Anything a provider offers beyond this is reachable by holding
// its SDK client directly, which is the honest escape hatch for a package whose
// selling point is being thin.
type Chat interface {
	// Complete returns the whole response.
	Complete(ctx context.Context, req Request) (*Response, error)

	// Stream calls fn with each delta as it arrives and returns the assembled
	// response. An error from fn stops the stream and is returned.
	Stream(ctx context.Context, req Request, fn func(Delta) error) (*Response, error)
}

// Embedder turns text into vectors.
type Embedder interface {
	// Embed returns one vector per input, in the same order.
	Embed(ctx context.Context, model string, inputs []string) ([][]float32, error)
}

// ErrNoAPIKey is returned when a provider was constructed without credentials.
//
// Returned at call time rather than panicking at construction, because an
// application that configures an optional provider it never uses should start.
var ErrNoAPIKey = errors.New("llm: no API key configured")

// validate catches the mistakes that would otherwise become a provider error
// with a worse message.
func (r Request) validate() error {
	if r.Model == "" {
		return errors.New("llm: no model specified")
	}
	if len(r.Messages) == 0 {
		return errors.New("llm: no messages")
	}
	for i, m := range r.Messages {
		if m.Role == RoleTool && m.ToolCallID == "" {
			return fmt.Errorf("llm: message %d is a tool result with no tool call id", i)
		}
	}
	return nil
}
