package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// anthropicChat is a Chat over anthropic-sdk-go.
type anthropicChat struct {
	client anthropic.Client
	key    string
}

// Anthropic returns a client for Anthropic's API.
//
// opts are passed through to the SDK, which is how a caller reaches Bedrock,
// Vertex, a gateway or a test server:
//
//	llm.Anthropic(key, option.WithBaseURL(server.URL))
func Anthropic(apiKey string, opts ...option.RequestOption) Chat {
	all := make([]option.RequestOption, 0, len(opts)+1)
	if apiKey != "" {
		all = append(all, option.WithAPIKey(apiKey))
	}
	all = append(all, opts...)

	return &anthropicChat{client: anthropic.NewClient(all...), key: apiKey}
}

func (a *anthropicChat) Complete(ctx context.Context, req Request) (*Response, error) {
	if a.key == "" {
		return nil, ErrNoAPIKey
	}
	if err := req.validate(); err != nil {
		return nil, err
	}

	ctx, done := startSpan(ctx, "anthropic", req)

	params, err := a.params(req)
	if err != nil {
		done(nil, err)
		return nil, err
	}

	message, err := a.client.Messages.New(ctx, params)
	if err != nil {
		done(nil, err)
		return nil, fmt.Errorf("llm: anthropic: %w", err)
	}

	out := &Response{
		StopReason: string(message.StopReason),
		Model:      string(message.Model),
		Usage: Usage{
			InputTokens:       int(message.Usage.InputTokens),
			OutputTokens:      int(message.Usage.OutputTokens),
			CachedInputTokens: int(message.Usage.CacheReadInputTokens),
		},
	}

	for _, block := range message.Content {
		switch block.Type {
		case "text":
			out.Text += block.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: json.RawMessage(block.Input),
			})
		}
	}

	if req.Schema != nil {
		structuredText(out, req.Schema.Name)
	}

	done(out, nil)
	return out, nil
}

func (a *anthropicChat) Stream(ctx context.Context, req Request, fn func(Delta) error) (*Response, error) {
	if a.key == "" {
		return nil, ErrNoAPIKey
	}
	if err := req.validate(); err != nil {
		return nil, err
	}

	ctx, done := startSpan(ctx, "anthropic", req)

	params, err := a.params(req)
	if err != nil {
		done(nil, err)
		return nil, err
	}

	stream := a.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	// The SDK's own accumulator reassembles the message, including tool inputs
	// that arrive as JSON fragments.
	var message anthropic.Message

	out := &Response{}
	emitted := 0

	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			done(out, err)
			return out, fmt.Errorf("llm: anthropic stream: %w", err)
		}

		if delta := event.AsContentBlockDelta(); delta.Type == "content_block_delta" {
			if text := delta.Delta.Text; text != "" {
				out.Text += text
				if err := fn(Delta{Text: text}); err != nil {
					done(out, err)
					return out, err
				}
			}
		}

		// A tool call is emitted when its block closes, which is the point its
		// input JSON is whole.
		if event.Type == "content_block_stop" {
			for _, block := range message.Content[emitted:] {
				emitted++
				if block.Type != "tool_use" {
					continue
				}

				call := ToolCall{ID: block.ID, Name: block.Name, Arguments: json.RawMessage(block.Input)}
				out.ToolCalls = append(out.ToolCalls, call)

				if err := fn(Delta{ToolCall: &call}); err != nil {
					done(out, err)
					return out, err
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		done(out, err)
		return out, fmt.Errorf("llm: anthropic stream: %w", err)
	}

	out.StopReason = string(message.StopReason)
	out.Model = string(message.Model)
	out.Usage = Usage{
		InputTokens:       int(message.Usage.InputTokens),
		OutputTokens:      int(message.Usage.OutputTokens),
		CachedInputTokens: int(message.Usage.CacheReadInputTokens),
	}

	if req.Schema != nil {
		structuredText(out, req.Schema.Name)
	}

	done(out, nil)
	return out, nil
}

func (a *anthropicChat) params(req Request) (anthropic.MessageNewParams, error) {
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))

	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Text)))

		case RoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			if m.Text != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Text))
			}
			for _, call := range m.ToolCalls {
				var input any
				if len(call.Arguments) > 0 {
					if err := json.Unmarshal(call.Arguments, &input); err != nil {
						return anthropic.MessageNewParams{}, fmt.Errorf("llm: tool call %q arguments: %w", call.Name, err)
					}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(call.ID, input, call.Name))
			}
			messages = append(messages, anthropic.NewAssistantMessage(blocks...))

		case RoleTool:
			// A tool result is a user-role message here, not its own role.
			// Providers disagree about this and it is exactly the kind of
			// difference this package exists to absorb.
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Text, false)))

		default:
			return anthropic.MessageNewParams{}, fmt.Errorf("llm: unknown role %q", m.Role)
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		// Required by this API, unlike OpenAI's. Defaulting rather than
		// erroring is what keeps the same Request working against both.
		maxTokens = DefaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		Messages:  messages,
		MaxTokens: int64(maxTokens),
	}

	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req.Temperature != nil {
		params.Temperature = param.NewOpt(*req.Temperature)
	}

	for _, tool := range req.Tools {
		schema, err := decodeSchema(tool.Schema)
		if err != nil {
			return anthropic.MessageNewParams{}, fmt.Errorf("llm: tool %q schema: %w", tool.Name, err)
		}

		input := anthropic.ToolInputSchemaParam{Properties: schema["properties"]}
		if required, ok := schema["required"].([]any); ok {
			for _, r := range required {
				if name, ok := r.(string); ok {
					input.Required = append(input.Required, name)
				}
			}
		}

		definition := anthropic.ToolParam{Name: tool.Name, InputSchema: input}
		if tool.Description != "" {
			definition.Description = param.NewOpt(tool.Description)
		}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &definition})
	}

	// Structured output, without a structured-output API.
	//
	// Anthropic has no response_format. The documented way to get JSON is a
	// single-tool request the model is forced into, and the tool's input *is*
	// the structured object. Doing that here rather than making the caller do
	// it is the difference between one Request working on both providers and
	// two code paths in every application.
	if req.Schema != nil {
		schema, err := decodeSchema(req.Schema.Definition)
		if err != nil {
			return anthropic.MessageNewParams{}, fmt.Errorf("llm: response schema: %w", err)
		}

		input := anthropic.ToolInputSchemaParam{Properties: schema["properties"]}
		if required, ok := schema["required"].([]any); ok {
			for _, r := range required {
				if name, ok := r.(string); ok {
					input.Required = append(input.Required, name)
				}
			}
		}

		name := req.Schema.Name
		if name == "" {
			name = "respond"
		}

		definition := anthropic.ToolParam{Name: name, InputSchema: input}
		if req.Schema.Description != "" {
			definition.Description = param.NewOpt(req.Schema.Description)
		}

		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &definition})
		params.ToolChoice = anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: name},
		}
	}

	return params, nil
}

// structuredText lifts a forced tool call back into Response.Text, so a caller
// asking for structured output gets JSON in the same field on both providers.
func structuredText(res *Response, schemaName string) {
	if res == nil || res.Text != "" || len(res.ToolCalls) == 0 {
		return
	}
	for _, call := range res.ToolCalls {
		if schemaName == "" || call.Name == schemaName {
			res.Text = string(call.Arguments)
			return
		}
	}
}
