package llm

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Token accounting, as span attributes, and nothing else.
//
// The issue this implements is explicit that cost *control* stays out: the
// pattern has not converged, and a framework that shipped a budget enforcer in
// 2026 would be shipping somebody's guess. Emitting the numbers onto the span
// that already exists is the part that is settled -- every observability
// backend can then group, sum and alert on them, which is where that decision
// belongs.
//
// The attribute names follow OpenTelemetry's `gen_ai.*` semantic conventions
// rather than being invented here, so an existing dashboard understands them.
// They are still experimental upstream; when they settle, this is the one file
// to change.

const tracerName = "github.com/jimmitjoo/tjo/llm"

// startSpan opens a span for one model call and returns the function that
// closes it.
//
// The returned function takes the response so usage can be recorded when it is
// known, which is after the call rather than before it. Passing a nil response
// records the error and nothing else.
func startSpan(ctx context.Context, provider string, req Request) (context.Context, func(*Response, error)) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "chat "+req.Model,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("gen_ai.system", provider),
			attribute.String("gen_ai.operation.name", "chat"),
			attribute.String("gen_ai.request.model", req.Model),
		),
	)

	if req.MaxTokens > 0 {
		span.SetAttributes(attribute.Int("gen_ai.request.max_tokens", req.MaxTokens))
	}
	if req.Temperature != nil {
		span.SetAttributes(attribute.Float64("gen_ai.request.temperature", *req.Temperature))
	}
	if len(req.Tools) > 0 {
		span.SetAttributes(attribute.Int("gen_ai.request.tool_count", len(req.Tools)))
	}

	return ctx, func(res *Response, err error) {
		defer span.End()

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		if res == nil {
			return
		}

		span.SetAttributes(
			attribute.Int("gen_ai.usage.input_tokens", res.Usage.InputTokens),
			attribute.Int("gen_ai.usage.output_tokens", res.Usage.OutputTokens),
		)

		// Reported separately rather than folded into the input count. It is
		// the difference between a cheap call and an expensive one, and a
		// dashboard that cannot see it cannot explain a bill.
		if res.Usage.CachedInputTokens > 0 {
			span.SetAttributes(attribute.Int("gen_ai.usage.cached_input_tokens", res.Usage.CachedInputTokens))
		}
		if res.Model != "" {
			span.SetAttributes(attribute.String("gen_ai.response.model", res.Model))
		}
		if res.StopReason != "" {
			span.SetAttributes(attribute.String("gen_ai.response.finish_reason", res.StopReason))
		}
		if len(res.ToolCalls) > 0 {
			span.SetAttributes(attribute.Int("gen_ai.response.tool_call_count", len(res.ToolCalls)))
		}

		// Prompts and completions are deliberately not recorded. They are the
		// user's data, they are frequently the most sensitive thing an
		// application handles, and a tracing backend is not where anybody
		// expects to find them. The conventions make this opt-in upstream too;
		// an application that wants it can add the attributes itself, having
		// decided that on purpose.
	}
}
