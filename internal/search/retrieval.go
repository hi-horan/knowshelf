package search

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"knowshelf/internal/models"
	"knowshelf/internal/store"
)

const (
	defaultBackendLimit = 20

	retrievalBM25               = "bm25"
	retrievalVector             = "vector"
	retrievalRewrittenBM25      = "rewritten_bm25"
	retrievalRewrittenVector    = "rewritten_vector"
	retrievalHypotheticalAnswer = "hypothetical_answer"
)

type retrievalResults struct {
	OriginalBM25       []store.Candidate
	RewrittenBM25      [][]store.Candidate
	OriginalVector     []store.Candidate
	RewrittenVector    [][]store.Candidate
	HypotheticalAnswer []store.Candidate
}

func (s *Service) retrieveHybrid(ctx context.Context, opts Options) (retrievalResults, error) {
	var results retrievalResults
	// Pipeline 1: 用户问题用于精确 BM25 召回。
	initialBM25, err := s.db.SearchBM25(ctx, s.segment, opts.Question, defaultBackendLimit, opts.BookID)
	if err != nil {
		return retrievalResults{}, err
	}
	results.OriginalBM25 = initialBM25

	// Pipeline 2: 外部 LLM 改写问题用于额外 BM25 召回。
	for _, query := range opts.RewrittenQueries {
		bm25Results, err := s.db.SearchBM25(ctx, s.segment, query, defaultBackendLimit, opts.BookID)
		if err != nil {
			return retrievalResults{}, err
		}
		if len(bm25Results) > 0 {
			setRetrievalType(bm25Results, retrievalRewrittenBM25)
			results.RewrittenBM25 = append(results.RewrittenBM25, bm25Results)
		}
	}

	// Pipeline 3: 用户问题、改写问题和假设答案分别走向量检索。
	results.OriginalVector, err = s.retrieveVector(ctx, opts.Question, retrievalVector, opts)
	if err != nil {
		return retrievalResults{}, err
	}
	for _, query := range opts.RewrittenQueries {
		vectorResults, err := s.retrieveVector(ctx, query, retrievalRewrittenVector, opts)
		if err != nil {
			return retrievalResults{}, err
		}
		if len(vectorResults) > 0 {
			results.RewrittenVector = append(results.RewrittenVector, vectorResults)
		}
	}
	results.HypotheticalAnswer, err = s.retrieveVector(ctx, opts.HypotheticalAnswer, retrievalHypotheticalAnswer, opts)
	if err != nil {
		return retrievalResults{}, err
	}
	return results, nil
}

func setRetrievalType(candidates []store.Candidate, retrievalType string) {
	for i := range candidates {
		candidates[i].RetrievalType = retrievalType
	}
}

func (s *Service) retrieveVector(ctx context.Context, query string, retrievalType string, opts Options) ([]store.Candidate, error) {
	startedAt := time.Now()
	if s.embedder == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	vectors, err := s.embedder.EmbedStrings(ctx, []string{s.embedder.FormatQuery(query)})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("embedding result count mismatch: got %d, want 1", len(vectors))
	}
	if len(vectors[0]) == 0 {
		return nil, nil
	}
	results, err := s.db.SearchVector(ctx, models.Float64ToFloat32(vectors[0]), defaultBackendLimit, retrievalType, query, opts.BookID)
	if err != nil {
		return nil, err
	}
	s.log().LogAttrs(ctx, slog.LevelDebug, "vector retrieval completed",
		slog.String("retrieval_type", retrievalType),
		slog.Int("candidate_count", len(results)),
		slog.Int64("duration_ms", durationMillis(startedAt)))
	return results, nil
}

func (r retrievalResults) listCount() int {
	count := 0
	if len(r.OriginalBM25) > 0 {
		count++
	}
	count += nonEmptyCandidateListCount(r.RewrittenBM25)
	if len(r.OriginalVector) > 0 {
		count++
	}
	count += nonEmptyCandidateListCount(r.RewrittenVector)
	if len(r.HypotheticalAnswer) > 0 {
		count++
	}
	return count
}

func nonEmptyCandidateListCount(lists [][]store.Candidate) int {
	count := 0
	for _, list := range lists {
		if len(list) > 0 {
			count++
		}
	}
	return count
}
