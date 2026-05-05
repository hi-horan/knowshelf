package store

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"time"
	"unicode/utf8"

	"knowshelf/internal/segment"
)

const bm25FetchMultiplier = 3
const vectorFetchMultiplier = 3

// SearchBM25 执行基于 gse 预分词的 FTS5 BM25 检索。
func (db *DB) SearchBM25(ctx context.Context, seg *segment.Segmenter, query string, limit int, bookID int64) ([]Candidate, error) {
	if limit <= 0 {
		limit = 20
	}
	terms := seg.QueryTerms(query)
	ftsQuery := segment.BuildFTS5Query(terms)
	if ftsQuery == "" {
		return nil, nil
	}
	db.log().LogAttrs(ctx, slog.LevelDebug, "fts query info",
		slog.Int("query_term_count", len(terms)),
		slog.Int("fts_query_chars", utf8.RuneCountInString(ftsQuery)))

	args := []any{ftsQuery}
	bookFilter := ""
	if bookID > 0 {
		bookFilter = " AND c_filter.book_id = ?"
		args = append(args, bookID)
	}
	args = append(args, max(limit*bm25FetchMultiplier, limit))
	sqlText := `
		WITH matches AS (
			SELECT chunks_fts.rowid, bm25(chunks_fts, 0.2, 1.3, 1.5) AS bm25_score
			FROM chunks_fts
			JOIN chunks c_filter ON c_filter.id = chunks_fts.rowid
			JOIN books b_filter ON b_filter.id = c_filter.book_id
			WHERE chunks_fts MATCH ?
				AND b_filter.active = 1
				` + bookFilter + `
			ORDER BY bm25_score ASC
			LIMIT ?
		)
		SELECT
			c.id, c.book_id, c.parent_chunk_id, c.title, c.heading_path, c.text,
			p.id, p.book_id, p.title, p.heading_path, p.text,
			b.title, b.source_path, m.bm25_score
		FROM matches m
		JOIN chunks c ON c.id = m.rowid
		JOIN parent_chunks p ON p.id = c.parent_chunk_id
		JOIN books b ON b.id = c.book_id
		WHERE b.active = 1
	`
	sqlText += ` ORDER BY m.bm25_score ASC LIMIT ?`
	args = append(args, limit)

	rows, err := db.query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("bm25 search: %w", err)
	}
	defer rows.Close()

	var results []Candidate
	rank := 1
	for rows.Next() {
		var c Chunk
		var p ParentChunk
		var bookTitle, sourcePath string
		var bm25Score float64
		if err := rows.Scan(
			&c.ID, &c.BookID, &c.ParentChunkID, &c.Title, &c.HeadingPath, &c.Text,
			&p.ID, &p.BookID, &p.Title, &p.HeadingPath, &p.Text,
			&bookTitle, &sourcePath, &bm25Score,
		); err != nil {
			return nil, fmt.Errorf("scan bm25 result: %w", err)
		}
		score := math.Abs(bm25Score) / (1 + math.Abs(bm25Score))
		results = append(results, Candidate{
			Chunk:         c,
			ParentChunk:   p,
			BookID:        c.BookID,
			BookTitle:     bookTitle,
			SourcePath:    sourcePath,
			Score:         score,
			Source:        "bm25",
			RetrievalType: "bm25",
			Query:         query,
			Rank:          rank,
		})
		rank++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bm25 results: %w", err)
	}
	return results, nil
}

// PendingEmbeddingChunks 返回没有 embedding 的 active chunk。
func (db *DB) PendingEmbeddingChunks(ctx context.Context, limit int) ([]PendingChunk, error) {
	sqlText := `
		SELECT c.id, c.book_id, c.title, c.heading_path, c.text
		FROM chunks c
		JOIN books b ON b.id = c.book_id
		LEFT JOIN embeddings e ON e.chunk_id = c.id
		WHERE b.active = 1 AND e.chunk_id IS NULL
		ORDER BY b.id, c.id
	`
	args := []any{}
	if limit > 0 {
		sqlText += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("pending embeddings: %w", err)
	}
	defer rows.Close()
	var chunks []PendingChunk
	for rows.Next() {
		var chunk PendingChunk
		if err := rows.Scan(&chunk.ID, &chunk.BookID, &chunk.Title, &chunk.HeadingPath, &chunk.Text); err != nil {
			return nil, fmt.Errorf("scan pending chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending chunks: %w", err)
	}
	return chunks, nil
}

// SaveEmbedding 保存某个 chunk 的向量。
func (db *DB) SaveEmbedding(ctx context.Context, chunkID int64, vector []float32) error {
	blob, err := encodeVector(vector)
	if err != nil {
		return err
	}

	tx, err := db.beginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin save embedding transaction: %w", err)
	}
	defer rollback(tx)

	bookID, err := chunkBookIDTx(ctx, tx, chunkID)
	if err != nil {
		return err
	}
	_, err = tx.exec(ctx, `
		INSERT INTO embeddings(chunk_id, embedded_at)
		VALUES (?, ?)
		ON CONFLICT(chunk_id) DO UPDATE SET
			embedded_at = excluded.embedded_at
	`, chunkID, time.Now().UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("save embedding: %w", err)
	}
	if err := saveVectorIndexTx(ctx, tx, chunkID, bookID, len(vector), blob); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit save embedding: %w", err)
	}
	return nil
}

// SearchVector 使用 sqlite-vec 向量索引执行 KNN 检索。
func (db *DB) SearchVector(ctx context.Context, queryVector []float32, limit int, queryType string, query string, bookID int64) ([]Candidate, error) {
	if limit <= 0 {
		limit = 20
	}
	queryBlob, err := encodeVector(queryVector)
	if err != nil {
		return nil, err
	}
	vectorLimit := max(limit*vectorFetchMultiplier, limit)
	args := []any{queryBlob}
	bookFilter := ""
	if bookID > 0 {
		bookFilter = " AND book_id = ?"
		args = append(args, bookID)
	}
	args = append(args, vectorLimit, limit)
	sqlText := `
		WITH vector_matches AS (
			SELECT rowid, distance
			FROM chunk_vectors
			WHERE embedding MATCH ?
				` + bookFilter + `
			ORDER BY distance
			LIMIT ?
		)
		SELECT
			c.id, c.book_id, c.parent_chunk_id, c.title, c.heading_path, c.text,
			p.id, p.book_id, p.title, p.heading_path, p.text,
			b.title, b.source_path, v.distance
		FROM vector_matches v
		JOIN chunks c ON c.id = v.rowid
		JOIN parent_chunks p ON p.id = c.parent_chunk_id
		JOIN books b ON b.id = c.book_id
		WHERE b.active = 1
	`
	sqlText += ` ORDER BY v.distance ASC LIMIT ?`
	rows, err := db.query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var results []Candidate
	for rows.Next() {
		var c Chunk
		var p ParentChunk
		var bookTitle, sourcePath string
		var distance float64
		if err := rows.Scan(
			&c.ID, &c.BookID, &c.ParentChunkID, &c.Title, &c.HeadingPath, &c.Text,
			&p.ID, &p.BookID, &p.Title, &p.HeadingPath, &p.Text,
			&bookTitle, &sourcePath, &distance,
		); err != nil {
			return nil, fmt.Errorf("scan vector result: %w", err)
		}
		score := vectorDistanceScore(distance)
		if score <= 0 {
			continue
		}
		results = append(results, Candidate{
			Chunk:         c,
			ParentChunk:   p,
			BookID:        c.BookID,
			BookTitle:     bookTitle,
			SourcePath:    sourcePath,
			Score:         score,
			Source:        "vector",
			RetrievalType: queryType,
			Query:         query,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vector results: %w", err)
	}
	for i := range results {
		results[i].Rank = i + 1
	}
	return results, nil
}

func vectorDistanceScore(distance float64) float64 {
	return max(0, min(1, 1-distance))
}

func encodeVector(vector []float32) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, len(vector)*4))
	for _, value := range vector {
		if err := binary.Write(buf, binary.LittleEndian, value); err != nil {
			return nil, fmt.Errorf("encode vector: %w", err)
		}
	}
	return buf.Bytes(), nil
}
