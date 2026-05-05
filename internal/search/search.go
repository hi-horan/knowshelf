// Package search 实现 BM25 与向量的混合召回和排序。
package search

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"knowshelf/internal/config"
	"knowshelf/internal/models"
	"knowshelf/internal/segment"
	"knowshelf/internal/store"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	maxRewrittenQueries = 3
)

type Options struct {
	Question           string
	RewrittenQueries   []string
	HypotheticalAnswer string
	BookID             int64
	Limit              int
	DisableRerank      bool
}

type Result struct {
	ChunkID       int64   `json:"chunk_id"`
	ParentChunkID int64   `json:"parent_chunk_id"`
	BookID        int64   `json:"book_id"`
	BookTitle     string  `json:"book_title"`
	SourcePath    string  `json:"source_path"`
	HeadingPath   string  `json:"heading_path"`
	Score         float64 `json:"score"`
	Text          string  `json:"text,omitempty"`
	Explain       Explain `json:"explain"`
}

type Explain struct {
	RRFRank       int            `json:"rrf_rank"`
	RRFScore      float64        `json:"rrf_score"`
	RerankScore   float64        `json:"rerank_score"`
	BlendedScore  float64        `json:"blended_score"`
	Contributions []Contribution `json:"contributions"`
}

type Contribution struct {
	Source        string  `json:"source"`
	RetrievalType string  `json:"retrieval_type"`
	Query         string  `json:"query"`
	Rank          int     `json:"rank"`
	Weight        float64 `json:"weight"`
	BackendScore  float64 `json:"backend_score"`
	RRFScore      float64 `json:"rrf_score"`
}

type Service struct {
	db       *store.DB
	segment  *segment.Segmenter
	embedder models.Embedder
	reranker models.Reranker
	config   config.SearchConfig
	logger   *slog.Logger
}

type Option func(*Service) error

func WithEmbedder(embedder models.Embedder) Option {
	return func(s *Service) error {
		s.embedder = embedder
		return nil
	}
}

func WithReranker(reranker models.Reranker) Option {
	return func(s *Service) error {
		s.reranker = reranker
		return nil
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(s *Service) error {
		if logger != nil {
			s.logger = logger
		}
		return nil
	}
}

func NewService(db *store.DB, seg *segment.Segmenter, cfg config.SearchConfig, opts ...Option) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("search db is nil")
	}
	if seg == nil {
		return nil, fmt.Errorf("search segmenter is nil")
	}
	service := &Service{
		db:      db,
		segment: seg,
		config:  cfg,
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		if err := opt(service); err != nil {
			return nil, err
		}
	}
	return service, nil
}

// Search 执行唯一的混合检索流程：
// 1. 用户问题 BM25；2. 外部改写问题 BM25；3. 用户问题/改写问题/假设答案向量召回；
// 4. RRF 融合并截断候选；5. 使用已存 chunk 作为最佳片段；6. 可选 rerank；
// 7. RRF 位置分与 rerank 分融合；8. 过滤分数并截断结果。
func (s *Service) Search(ctx context.Context, opts Options) (results []Result, err error) {
	startedAt := time.Now()
	var listCount, rankedCount, candidateCount int
	limit := s.resultLimit(opts)
	rerank := s.shouldRerank(opts)
	ctx, span := otel.Tracer("knowshelf/internal/search").Start(ctx, "search")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(
			attribute.Int("search.result_count", len(results)),
			attribute.Int("search.list_count", listCount),
			attribute.Int("search.ranked_count", rankedCount),
			attribute.Int("search.candidate_count", candidateCount),
		)
		logSearchCompletion(ctx, s.log(), startedAt, opts, s.config, limit, rerank, len(results), listCount, rankedCount, candidateCount, s.embedder != nil, s.reranker != nil, err)
		span.End()
	}()

	if s.db == nil || s.segment == nil {
		return nil, fmt.Errorf("search service is not initialized")
	}
	if err := normalizeOptions(&opts); err != nil {
		return nil, err
	}
	limit = s.resultLimit(opts)
	rerank = s.shouldRerank(opts)
	span.SetAttributes(
		attribute.Int("search.limit", limit),
		attribute.Int("search.candidate_limit", s.config.CandidateLimit),
		attribute.Int("search.rewritten_query_count", len(opts.RewrittenQueries)),
		attribute.Int64("search.book_id", opts.BookID),
		attribute.Bool("search.rerank", rerank),
	)
	if opts.Question == "" {
		return nil, fmt.Errorf("question is empty")
	}

	retrieved, err := s.retrieveHybrid(ctx, opts)
	if err != nil {
		return nil, err
	}
	listCount = retrieved.listCount()
	if listCount == 0 {
		return nil, nil
	}
	ranked := rrf(retrieved, s.config.RRFWeights)
	rankedCount = len(ranked)
	if rankedCount == 0 {
		return nil, nil
	}
	if len(ranked) > s.config.CandidateLimit {
		ranked = ranked[:s.config.CandidateLimit]
	}
	candidateCount = len(ranked)
	if !rerank {
		results = resultsWithoutRerank(ranked)
	} else {
		results, err = s.rerank(ctx, ranked, opts)
		if err != nil {
			return nil, err
		}
	}
	return s.finalizeResults(results, limit), nil
}

func normalizeOptions(opts *Options) error {
	if len(opts.RewrittenQueries) > maxRewrittenQueries {
		return fmt.Errorf("rewrittenQueries supports at most %d items", maxRewrittenQueries)
	}
	if opts.BookID < 0 {
		return fmt.Errorf("bookID must be positive")
	}
	opts.Question = strings.TrimSpace(opts.Question)
	opts.HypotheticalAnswer = strings.TrimSpace(opts.HypotheticalAnswer)
	opts.RewrittenQueries = normalizeRewrittenQueries(opts.Question, opts.RewrittenQueries)
	return nil
}

func normalizeRewrittenQueries(question string, queries []string) []string {
	seen := make(map[string]struct{}, len(queries)+1)
	if question != "" {
		seen[strings.ToLower(question)] = struct{}{}
	}
	out := make([]string, 0, len(queries))
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		key := strings.ToLower(query)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, query)
	}
	return out
}

func (s *Service) resultLimit(opts Options) int {
	if opts.Limit > 0 {
		return opts.Limit
	}
	return s.config.DefaultLimit
}

func (s *Service) shouldRerank(opts Options) bool {
	return s.config.EnableRerank && !opts.DisableRerank && s.reranker != nil
}

func (s *Service) finalizeResults(results []Result, limit int) []Result {
	if len(results) == 0 {
		return results
	}
	out := results[:0]
	for _, result := range results {
		if result.Score < s.config.MinScore {
			continue
		}
		out = append(out, result)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
