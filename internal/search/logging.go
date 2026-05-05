package search

import (
	"context"
	"log/slog"
	"time"
	"unicode/utf8"

	"knowshelf/internal/config"
)

func (s *Service) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

func logSearchCompletion(ctx context.Context, logger *slog.Logger, startedAt time.Time, opts Options, cfg config.SearchConfig, limit int, rerank bool, resultCount, listCount, rankedCount, candidateCount int, hasEmbedder, hasReranker bool, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	level := slog.LevelInfo
	message := "search completed"
	// 查询文本可能包含用户笔记或书中原文，只记录长度和结构化计数，避免把内容写入日志。
	attrs := []slog.Attr{
		slog.Int64("duration_ms", durationMillis(startedAt)),
		slog.Int("result_count", resultCount),
		slog.Int("list_count", listCount),
		slog.Int("ranked_count", rankedCount),
		slog.Int("candidate_count", candidateCount),
		slog.Int("question_chars", utf8.RuneCountInString(opts.Question)),
		slog.Int("rewritten_query_count", len(opts.RewrittenQueries)),
		slog.Int("hypothetical_answer_chars", utf8.RuneCountInString(opts.HypotheticalAnswer)),
		slog.Int64("book_id", opts.BookID),
		slog.Int("limit", limit),
		slog.Int("candidate_limit", cfg.CandidateLimit),
		slog.Bool("rerank_requested", rerank),
		slog.Bool("has_embedder", hasEmbedder),
		slog.Bool("has_reranker", hasReranker),
	}
	if err != nil {
		level = slog.LevelError
		message = "search failed"
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	logger.LogAttrs(ctx, level, message, attrs...)
}

func durationMillis(startedAt time.Time) int64 {
	return time.Since(startedAt).Milliseconds()
}
