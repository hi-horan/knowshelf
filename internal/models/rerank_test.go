package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"knowshelf/internal/config"
)

func TestOpenAIRerankerSendsPlainDocuments(t *testing.T) {
	var got rerankRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.2},{"index":9,"relevance_score":1}]}`))
	}))
	defer server.Close()

	reranker, err := NewOpenAIReranker(&config.ModelConfig{
		Provider: "test",
		Model:    "test-rerank",
		BaseURL:  server.URL,
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("NewOpenAIReranker() error = %v", err)
	}

	scores, err := reranker.Rerank(context.Background(), "query", []string{"parent text 1", "parent text 2"})
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}

	wantDocuments := []string{"parent text 1", "parent text 2"}
	if !reflect.DeepEqual(got.Documents, wantDocuments) {
		t.Fatalf("documents = %#v, want %#v", got.Documents, wantDocuments)
	}
	if got.Model != "test-rerank" || got.Query != "query" || got.TopN != 2 {
		t.Fatalf("request = %#v", got)
	}

	wantScores := []RerankScore{
		{Index: 1, Score: 0.9},
		{Index: 0, Score: 0.2},
	}
	if !reflect.DeepEqual(scores, wantScores) {
		t.Fatalf("scores = %#v, want %#v", scores, wantScores)
	}
}
