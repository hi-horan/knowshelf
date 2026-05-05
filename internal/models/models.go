// Package models 定义 embedding 和 rerank provider 接口。
package models

import (
	"context"

	"github.com/cloudwego/eino/components/embedding"
)

type EmbedInput struct {
	Title string
	Text  string
}

type Embedder interface {
	embedding.Embedder
	FormatQuery(text string) string
	FormatDocument(title, text string) string
}

type RerankScore struct {
	Index int
	Score float64
}

type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]RerankScore, error)
}

func Float64ToFloat32(vector []float64) []float32 {
	out := make([]float32, len(vector))
	for i, value := range vector {
		out[i] = float32(value)
	}
	return out
}
