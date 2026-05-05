package search

import (
	"context"
	"testing"

	"knowshelf/internal/config"
	"knowshelf/internal/models"
)

type testReranker struct{}

func (testReranker) Rerank(context.Context, string, []string) ([]models.RerankScore, error) {
	return nil, nil
}

func TestServiceUsesSearchConfig(t *testing.T) {
	service := &Service{
		config: config.SearchConfig{
			DefaultLimit: 2,
			MinScore:     0.5,
			EnableRerank: true,
		},
		reranker: testReranker{},
	}

	results := service.finalizeResults([]Result{
		{Score: 0.9},
		{Score: 0.4},
		{Score: 0.8},
	}, service.resultLimit(Options{}))

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Score != 0.9 || results[1].Score != 0.8 {
		t.Fatalf("results = %#v, want scores filtered by config min_score", results)
	}
	if !service.shouldRerank(Options{}) {
		t.Fatal("shouldRerank() = false, want config enable_rerank to apply")
	}
	if service.shouldRerank(Options{DisableRerank: true}) {
		t.Fatal("shouldRerank() = true, want request disable to override")
	}
}
