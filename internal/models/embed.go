package models

import (
	"context"
	"fmt"
	"time"

	"knowshelf/internal/config"

	openaiembed "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"
)

const defaultTimeout = 2 * time.Minute

func NewEmbedder(ctx context.Context, cfg *config.ModelConfig) (Embedder, error) {
	switch cfg.Provider {
	case "openai":
		return newOpenAIEmbedder(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", cfg.Provider)
	}
}

type einoEmbedder struct {
	model  string
	client embedding.Embedder
}

func (e *einoEmbedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	return e.client.EmbedStrings(ctx, texts, opts...)
}

func (e *einoEmbedder) FormatQuery(text string) string {
	return "task: search result | query: " + text
}

func (e *einoEmbedder) FormatDocument(title, text string) string {
	return "title: " + title + " | text: " + text
}

func newOpenAIEmbedder(ctx context.Context, cfg *config.ModelConfig) (Embedder, error) {
	openAIConfig := &openaiembed.EmbeddingConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		Timeout: timeoutDuration(cfg),
	}
	if cfg.Dimensions > 0 {
		openAIConfig.Dimensions = &cfg.Dimensions
	}
	client, err := openaiembed.NewEmbedder(ctx, openAIConfig)
	if err != nil {
		return nil, err
	}
	return &einoEmbedder{model: cfg.Model, client: client}, nil
}

func timeoutDuration(cfg *config.ModelConfig) time.Duration {
	if cfg.TimeoutMS <= 0 {
		return defaultTimeout
	}
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}
