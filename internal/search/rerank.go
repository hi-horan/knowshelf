package search

import (
	"context"
	"sort"
)

func (s *Service) rerank(ctx context.Context, ranked []rankedCandidate, opts Options) ([]Result, error) {
	documents := make([]string, 0, len(ranked))
	for _, item := range ranked {
		documents = append(documents, item.Candidate.ParentChunk.Text)
	}
	scores, err := s.reranker.Rerank(ctx, opts.Question, documents)
	if err != nil {
		return nil, err
	}
	scoreByIndex := make(map[int]float64, len(scores))
	for _, score := range scores {
		if score.Index < 0 || score.Index >= len(ranked) {
			continue
		}
		scoreByIndex[score.Index] = score.Score
	}
	results := make([]Result, 0, len(ranked))
	for i, item := range ranked {
		rrfRank := i + 1
		rerankScore := scoreByIndex[i]
		// RRF 前几名本身就是强信号，因此和 rerank 分数做融合，
		// 避免一次模型调用完全改写候选集排序。
		finalScore := blendedRerankScore(rrfRank, rerankScore)
		result := buildResult(item, finalScore, rerankScore, rrfRank)
		results = append(results, result)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results, nil
}
