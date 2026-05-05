package mcp

import (
	"context"
	"log/slog"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func (s *Server) loggingMiddleware() mcpsdk.Middleware {
	return func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			sessionID := requestSessionID(req)
			toolName := requestToolName(req)
			ctx, span := startMethodSpan(ctx, method, sessionID, toolName)
			defer span.End()

			attrs := methodLogAttrs(method, sessionID, toolName, req)
			s.log().LogAttrs(ctx, slog.LevelInfo, "mcp method started", attrs...)

			startedAt := time.Now()
			result, err := next(ctx, method, req)
			attrs = append(attrs, slog.Int64("duration_ms", durationMillis(startedAt)))
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				s.log().LogAttrs(ctx, slog.LevelError, "mcp method failed",
					append(attrs, slog.String("error", err.Error()))...)
				return result, err
			}

			attrs = append(attrs, resultLogAttrs(result)...)
			if toolResult, ok := result.(*mcpsdk.CallToolResult); ok && toolResult.IsError {
				span.SetStatus(codes.Error, "tool result is error")
				s.log().LogAttrs(ctx, slog.LevelWarn, "mcp method completed with tool error", attrs...)
				return result, nil
			}
			s.log().LogAttrs(ctx, slog.LevelInfo, "mcp method completed", attrs...)
			return result, nil
		}
	}
}

func (s *Server) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

func startToolSpan(ctx context.Context, tool string) (context.Context, trace.Span) {
	return otel.Tracer("knowshelf/internal/mcp").Start(ctx, "mcp."+tool,
		trace.WithAttributes(attribute.String("mcp.tool", tool)))
}

func startMethodSpan(ctx context.Context, method string, sessionID string, toolName string) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{attribute.String("mcp.method", method)}
	if sessionID != "" {
		attrs = append(attrs, attribute.String("mcp.session_id", sessionID))
	}
	if toolName != "" {
		attrs = append(attrs, attribute.String("mcp.tool", toolName))
	}
	return otel.Tracer("knowshelf/internal/mcp").Start(ctx, "mcp."+method, trace.WithAttributes(attrs...))
}

func methodLogAttrs(method string, sessionID string, toolName string, req mcpsdk.Request) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("method", method),
		slog.String("session_id", sessionID),
		slog.Bool("has_params", req != nil && req.GetParams() != nil),
	}
	if toolName != "" {
		attrs = append(attrs, slog.String("tool", toolName))
	}
	if callToolReq, ok := req.(*mcpsdk.CallToolRequest); ok && callToolReq.Params != nil {
		attrs = append(attrs, slog.Int("tool_args_bytes", len(callToolReq.Params.Arguments)))
	}
	return attrs
}

func resultLogAttrs(result mcpsdk.Result) []slog.Attr {
	attrs := []slog.Attr{slog.Bool("has_result", result != nil)}
	if toolResult, ok := result.(*mcpsdk.CallToolResult); ok {
		attrs = append(attrs,
			slog.Bool("tool_is_error", toolResult.IsError),
			slog.Bool("has_structured_content", toolResult.StructuredContent != nil),
		)
	}
	return attrs
}

func requestSessionID(req mcpsdk.Request) string {
	if req == nil || req.GetSession() == nil {
		return ""
	}
	return req.GetSession().ID()
}

func requestToolName(req mcpsdk.Request) string {
	callToolReq, ok := req.(*mcpsdk.CallToolRequest)
	if !ok || callToolReq.Params == nil {
		return ""
	}
	return callToolReq.Params.Name
}

func durationMillis(startedAt time.Time) int64 {
	return time.Since(startedAt).Milliseconds()
}
