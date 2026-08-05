package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
)

// openAI is a Chat and an Embedder over openai-go.
type openAI struct {
	client openai.Client
	key    string
}

// OpenAI returns a client for OpenAI's API.
//
// opts are passed through to the SDK, which is how a caller reaches an
// Azure deployment, a proxy, a self-hosted gateway or a test server:
//
//	llm.OpenAI(key, option.WithBaseURL(server.URL))
//
// Anything compatible with the chat completions API works, which is most
// things -- and being thin is what makes that true without this package
// listing them.
func OpenAI(apiKey string, opts ...option.RequestOption) Chat {
	return newOpenAI(apiKey, opts...)
}

// OpenAIEmbedder returns an Embedder for OpenAI's API.
func OpenAIEmbedder(apiKey string, opts ...option.RequestOption) Embedder {
	return newOpenAI(apiKey, opts...)
}

func newOpenAI(apiKey string, opts ...option.RequestOption) *openAI {
	all := make([]option.RequestOption, 0, len(opts)+1)
	if apiKey != "" {
		all = append(all, option.WithAPIKey(apiKey))
	}
	all = append(all, opts...)

	return &openAI{client: openai.NewClient(all...), key: apiKey}
}

func (o *openAI) Complete(ctx context.Context, req Request) (*Response, error) {
	if o.key == "" {
		return nil, ErrNoAPIKey
	}
	if err := req.validate(); err != nil {
		return nil, err
	}

	ctx, done := startSpan(ctx, "openai", req)

	params, err := o.params(req)
	if err != nil {
		done(nil, err)
		return nil, err
	}

	completion, err := o.client.Chat.Completions.New(ctx, params)
	if err != nil {
		done(nil, err)
		return nil, fmt.Errorf("llm: openai: %w", err)
	}
	if len(completion.Choices) == 0 {
		err := fmt.Errorf("llm: openai returned no choices")
		done(nil, err)
		return nil, err
	}

	choice := completion.Choices[0]

	out := &Response{
		Text:       choice.Message.Content,
		StopReason: choice.FinishReason,
		Model:      completion.Model,
		Usage: Usage{
			InputTokens:       int(completion.Usage.PromptTokens),
			OutputTokens:      int(completion.Usage.CompletionTokens),
			CachedInputTokens: int(completion.Usage.PromptTokensDetails.CachedTokens),
		},
	}

	for _, call := range choice.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: json.RawMessage(call.Function.Arguments),
		})
	}

	done(out, nil)
	return out, nil
}

func (o *openAI) Stream(ctx context.Context, req Request, fn func(Delta) error) (*Response, error) {
	if o.key == "" {
		return nil, ErrNoAPIKey
	}
	if err := req.validate(); err != nil {
		return nil, err
	}

	ctx, done := startSpan(ctx, "openai", req)

	params, err := o.params(req)
	if err != nil {
		done(nil, err)
		return nil, err
	}

	stream := o.client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	// The SDK's accumulator reassembles tool-call arguments, which arrive one
	// fragment at a time. Doing that here rather than in every caller is most
	// of the reason this method exists.
	var acc openai.ChatCompletionAccumulator

	out := &Response{}
	emitted := map[int]bool{}

	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)

		if len(chunk.Choices) > 0 {
			if text := chunk.Choices[0].Delta.Content; text != "" {
				out.Text += text
				if err := fn(Delta{Text: text}); err != nil {
					done(out, err)
					return out, err
				}
			}
		}

		// A finished tool call is emitted once, when its arguments are whole.
		// A half-parsed argument list is not something a caller can act on.
		if call, ok := acc.JustFinishedToolCall(); ok && !emitted[call.Index] {
			emitted[call.Index] = true

			tc := ToolCall{ID: call.ID, Name: call.Name, Arguments: json.RawMessage(call.Arguments)}
			out.ToolCalls = append(out.ToolCalls, tc)

			if err := fn(Delta{ToolCall: &tc}); err != nil {
				done(out, err)
				return out, err
			}
		}
	}

	if err := stream.Err(); err != nil {
		done(out, err)
		return out, fmt.Errorf("llm: openai stream: %w", err)
	}

	out.Model = acc.Model
	out.Usage = Usage{
		InputTokens:       int(acc.Usage.PromptTokens),
		OutputTokens:      int(acc.Usage.CompletionTokens),
		CachedInputTokens: int(acc.Usage.PromptTokensDetails.CachedTokens),
	}
	if len(acc.Choices) > 0 {
		out.StopReason = acc.Choices[0].FinishReason
		if out.Text == "" {
			out.Text = acc.Choices[0].Message.Content
		}
	}

	done(out, nil)
	return out, nil
}

func (o *openAI) Embed(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	if o.key == "" {
		return nil, ErrNoAPIKey
	}
	if model == "" {
		return nil, fmt.Errorf("llm: no embedding model specified")
	}
	if len(inputs) == 0 {
		return nil, nil
	}

	res, err := o.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(model),
		Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: inputs},
	})
	if err != nil {
		return nil, fmt.Errorf("llm: openai embed: %w", err)
	}

	// The API documents that embeddings come back in input order, but it also
	// returns an index on each one. Placing by index costs nothing and does not
	// depend on that promise holding.
	out := make([][]float32, len(inputs))
	for _, item := range res.Data {
		i := int(item.Index)
		if i < 0 || i >= len(out) {
			return nil, fmt.Errorf("llm: openai returned an embedding for input %d of %d", i, len(inputs))
		}
		vector := make([]float32, len(item.Embedding))
		for j, v := range item.Embedding {
			vector[j] = float32(v)
		}
		out[i] = vector
	}

	for i, v := range out {
		if v == nil {
			return nil, fmt.Errorf("llm: openai returned no embedding for input %d", i)
		}
	}
	return out, nil
}

func (o *openAI) params(req Request) (openai.ChatCompletionNewParams, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)

	if req.System != "" {
		messages = append(messages, openai.SystemMessage(req.System))
	}

	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			messages = append(messages, openai.UserMessage(m.Text))

		case RoleAssistant:
			assistant := openai.ChatCompletionAssistantMessageParam{}
			if m.Text != "" {
				assistant.Content.OfString = param.NewOpt(m.Text)
			}
			for _, call := range m.ToolCalls {
				assistant.ToolCalls = append(assistant.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: call.ID,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      call.Name,
							Arguments: string(call.Arguments),
						},
					},
				})
			}
			messages = append(messages, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})

		case RoleTool:
			messages = append(messages, openai.ToolMessage(m.Text, m.ToolCallID))

		default:
			return openai.ChatCompletionNewParams{}, fmt.Errorf("llm: unknown role %q", m.Role)
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(req.Model),
		Messages: messages,
	}

	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = param.NewOpt(int64(req.MaxTokens))
	}
	if req.Temperature != nil {
		params.Temperature = param.NewOpt(*req.Temperature)
	}

	for _, tool := range req.Tools {
		schema, err := decodeSchema(tool.Schema)
		if err != nil {
			return openai.ChatCompletionNewParams{}, fmt.Errorf("llm: tool %q schema: %w", tool.Name, err)
		}

		definition := shared.FunctionDefinitionParam{Name: tool.Name, Parameters: schema}
		if tool.Description != "" {
			definition.Description = param.NewOpt(tool.Description)
		}
		params.Tools = append(params.Tools, openai.ChatCompletionFunctionTool(definition))
	}

	if req.Schema != nil {
		var definition any
		if err := json.Unmarshal(req.Schema.Definition, &definition); err != nil {
			return openai.ChatCompletionNewParams{}, fmt.Errorf("llm: response schema: %w", err)
		}

		jsonSchema := shared.ResponseFormatJSONSchemaJSONSchemaParam{
			Name:   req.Schema.Name,
			Schema: definition,
		}
		if req.Schema.Description != "" {
			jsonSchema.Description = param.NewOpt(req.Schema.Description)
		}
		if req.Schema.Strict {
			jsonSchema.Strict = param.NewOpt(true)
		}

		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: jsonSchema,
				Type:       constant.JSONSchema("json_schema"),
			},
		}
	}

	return params, nil
}

// decodeSchema turns JSON Schema bytes into the map the SDK wants.
func decodeSchema(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
