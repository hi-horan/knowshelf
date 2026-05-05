package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSearchRRFWeights(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`search:
  rrf_weights:
    original_vector: 2.5
    original_bm25: 1.5
    rewritten_vector: 1.2
    rewritten_bm25: 1.0
    hypothetical_answer: 0.8
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	weights := cfg.Search.RRFWeights
	if weights.OriginalVector != 2.5 ||
		weights.OriginalBM25 != 1.5 ||
		weights.RewrittenVector != 1.2 ||
		weights.RewrittenBM25 != 1.0 ||
		weights.HypotheticalAnswer != 0.8 {
		t.Fatalf("RRFWeights = %#v", weights)
	}
}
