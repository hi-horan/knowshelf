package models

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"knowshelf/internal/config"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type OpenAIReranker struct {
	client openai.Client
	cfg    *config.ModelConfig
	logger *slog.Logger
}

type ClientOption func(*clientOptions) error

type clientOptions struct {
	logger *slog.Logger
}

func WithLogger(logger *slog.Logger) ClientOption {
	return func(opts *clientOptions) error {
		if logger != nil {
			opts.logger = logger
		}
		return nil
	}
}

func NewOpenAIReranker(cfg *config.ModelConfig, opts ...ClientOption) (*OpenAIReranker, error) {
	clientOpts, err := applyClientOptions(opts)
	if err != nil {
		return nil, err
	}
	requestOpts := []option.RequestOption{
		option.WithBaseURL(cfg.BaseURL),
		option.WithRequestTimeout(timeoutDuration(cfg)),
		option.WithAPIKey(cfg.APIKey),
	}
	for key, value := range cfg.Headers {
		requestOpts = append(requestOpts, option.WithHeader(key, value))
	}
	return &OpenAIReranker{
		client: openai.NewClient(requestOpts...),
		cfg:    cfg,
		logger: clientOpts.logger,
	}, nil
}

// Rerank 将 chunk 发送到配置的 rerank endpoint。
func (r *OpenAIReranker) Rerank(ctx context.Context, query string, documents []string) (scores []RerankScore, err error) {
	startedAt := time.Now()
	ctx, span := otel.Tracer("knowshelf/internal/models").Start(ctx, "rerank.openai",
		trace.WithAttributes(
			attribute.String("rerank.provider", r.cfg.Provider),
			attribute.String("rerank.model", r.cfg.Model),
			attribute.Int("rerank.doc_count", len(documents)),
		))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(attribute.Int("rerank.result_count", len(scores)))
		attrs := []slog.Attr{
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
			slog.Int("doc_count", len(documents)),
			slog.Int("result_count", len(scores)),
		}
		if err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
		}
		r.log().LogAttrs(ctx, slog.LevelDebug, "rerank completed", attrs...)
		span.End()
	}()

	if r.cfg.Model == "" {
		err := fmt.Errorf("rerank model is empty")
		r.log().LogAttrs(ctx, slog.LevelError, "rerank config invalid",
			slog.String("error", err.Error()),
			slog.Int("doc_count", len(documents)))
		return nil, err
	}
	payload := rerankRequest{
		Model:     r.cfg.Model,
		Query:     query,
		TopN:      len(documents),
		Documents: documents,
	}
	var parsed rerankResponse
	if err := r.client.Post(ctx, r.cfg.BaseURL, payload, &parsed); err != nil {
		r.log().LogAttrs(ctx, slog.LevelError, "rerank request failed",
			slog.String("provider", r.cfg.Provider),
			slog.String("model", r.cfg.Model),
			slog.String("endpoint", r.cfg.BaseURL),
			slog.String("error", err.Error()),
			slog.Int("doc_count", len(documents)))
		return nil, fmt.Errorf("rerank request: %w", err)
	}
	scores = make([]RerankScore, 0, len(parsed.Results))
	for _, item := range parsed.Results {
		if item.Index < 0 || item.Index >= len(documents) {
			continue
		}
		scores = append(scores, RerankScore{
			Index: item.Index,
			Score: item.RelevanceScore,
		})
	}
	return scores, nil
}

func applyClientOptions(opts []ClientOption) (clientOptions, error) {
	out := clientOptions{logger: slog.Default()}
	for _, opt := range opts {
		if err := opt(&out); err != nil {
			return clientOptions{}, err
		}
	}
	return out, nil
}

func (r *OpenAIReranker) log() *slog.Logger {
	if r.logger != nil {
		return r.logger
	}
	return slog.Default()
}

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type rerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}
