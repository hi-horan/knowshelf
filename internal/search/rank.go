package search

import (
	"sort"

	"knowshelf/internal/config"
	"knowshelf/internal/store"
)

const (
	rrfK = 60

	firstRankBonus = 0.05
	topThreeBonus  = 0.02
)

type rankedCandidate struct {
	Candidate     store.Candidate
	RRFScore      float64
	TopRank       int
	Contributions []Contribution
}

func rrf(results retrievalResults, weights config.RRFWeightsConfig) []rankedCandidate {
	// 多个 child 可能命中同一个 parent，容量按候选总数预估可以减少 map 扩容；
	// 实际唯一 parent 数通常更小，预估偏大只影响少量内存，不影响排序。
	byParent := make(map[int64]*rankedCandidate, candidateListSize(results))
	addList := func(list []store.Candidate, weight float64) {
		for rankIndex, candidate := range list {
			rank := rankIndex + 1
			// Backend score 只进入 explain；排序用 RRF contribution，避免把 BM25
			// 和 vector cosine distance 的不同分数尺度放在一起硬比。
			contribution := rrfContribution(rank, weight)
			parentID := candidate.ParentChunk.ID
			item := byParent[parentID]
			if item == nil {
				candidate.Rank = rank
				item = &rankedCandidate{
					Candidate: candidate,
					TopRank:   rank,
				}
				byParent[parentID] = item
			}
			item.RRFScore += contribution
			if rank < item.TopRank {
				item.TopRank = rank
				item.Candidate = candidate
			}
			item.Contributions = append(item.Contributions, Contribution{
				Source:        candidate.Source,
				RetrievalType: candidate.RetrievalType,
				Query:         candidate.Query,
				Rank:          rank,
				Weight:        weight,
				BackendScore:  candidate.Score,
				RRFScore:      contribution,
			})
		}
	}
	addList(results.OriginalVector, weights.OriginalVector)
	addList(results.OriginalBM25, weights.OriginalBM25)
	for _, list := range results.RewrittenVector {
		addList(list, weights.RewrittenVector)
	}
	for _, list := range results.RewrittenBM25 {
		addList(list, weights.RewrittenBM25)
	}
	addList(results.HypotheticalAnswer, weights.HypotheticalAnswer)

	out := make([]rankedCandidate, 0, len(byParent))
	for _, item := range byParent {
		// 头部命中给一个小 bonus，避免某个明确第一名被另一后端的大量中位结果淹没。
		item.RRFScore += topRankBonus(item.TopRank)
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].RRFScore > out[j].RRFScore
	})
	return out
}

func candidateListSize(results retrievalResults) int {
	size := len(results.OriginalBM25) + len(results.OriginalVector) + len(results.HypotheticalAnswer)
	size += candidateListsSize(results.RewrittenBM25)
	size += candidateListsSize(results.RewrittenVector)
	return size
}

func candidateListsSize(lists [][]store.Candidate) int {
	size := 0
	for _, list := range lists {
		size += len(list)
	}
	return size
}

func resultsWithoutRerank(ranked []rankedCandidate) []Result {
	results := make([]Result, 0, len(ranked))
	if len(ranked) == 0 {
		return results
	}
	maxRRFScore := ranked[0].RRFScore
	for i, item := range ranked {
		rrfRank := i + 1
		finalScore := normalizedRRFScore(item.RRFScore, maxRRFScore)
		results = append(results, buildResult(item, finalScore, 0, rrfRank))
	}
	return results
}

func buildResult(item rankedCandidate, finalScore float64, rerankScore float64, rrfRank int) Result {
	chunk := item.Candidate.Chunk
	parent := item.Candidate.ParentChunk
	return Result{
		ChunkID:       chunk.ID,
		ParentChunkID: parent.ID,
		BookID:        item.Candidate.BookID,
		BookTitle:     item.Candidate.BookTitle,
		SourcePath:    item.Candidate.SourcePath,
		HeadingPath:   parent.HeadingPath,
		Score:         finalScore,
		Text:          parent.Text,
		Explain: Explain{
			RRFRank:       rrfRank,
			RRFScore:      item.RRFScore,
			RerankScore:   rerankScore,
			BlendedScore:  finalScore,
			Contributions: item.Contributions,
		},
	}
}

func rrfContribution(rank int, weight float64) float64 {
	return weight / float64(rrfK+rank)
}

func topRankBonus(topRank int) float64 {
	switch {
	case topRank == 1:
		return firstRankBonus
	case topRank > 1 && topRank <= 3:
		return topThreeBonus
	default:
		return 0
	}
}

func normalizedRRFScore(rrfScore float64, maxRRFScore float64) float64 {
	if maxRRFScore <= 0 {
		return 0
	}
	return rrfScore / maxRRFScore
}

func rrfPositionScore(rrfRank int) float64 {
	return 1 / float64(rrfRank)
}

func rerankBlendWeight(rrfRank int) float64 {
	switch {
	case rrfRank <= 3:
		return 0.75
	case rrfRank <= 10:
		return 0.60
	default:
		return 0.40
	}
}

func blendedRerankScore(rrfRank int, rerankScore float64) float64 {
	rrfWeight := rerankBlendWeight(rrfRank)
	return rrfWeight*rrfPositionScore(rrfRank) + (1-rrfWeight)*rerankScore
}
