package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"knowshelf/internal/segment"
)

var headingRE = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

// ImportMarkdown 导入整本 Markdown 书并索引其 chunk。
func (db *DB) ImportMarkdown(ctx context.Context, seg *segment.Segmenter, opts ImportOptions) (ImportResult, error) {
	if seg == nil {
		return ImportResult{}, fmt.Errorf("segmenter is nil")
	}
	path := filepath.Join(opts.MarkdownDir, opts.Name)
	content, err := ReadMarkdownFile(path)
	if err != nil {
		return ImportResult{}, err
	}
	// content = cleanMarkdownContent(content)
	if strings.TrimSpace(content) == "" {
		return ImportResult{}, fmt.Errorf("markdown file is empty: %s", path)
	}

	title := extractTitle(content, opts.Name)
	chunks, err := chunkMarkdown(ctx, content, title)
	if err != nil {
		return ImportResult{}, err
	}
	if len(chunks.Children) == 0 {
		return ImportResult{}, fmt.Errorf("no chunks produced for %s", path)
	}

	tx, err := db.beginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, fmt.Errorf("begin import transaction: %w", err)
	}
	defer rollback(tx)

	existingID, err := findExistingBook(ctx, tx, path)
	if err != nil {
		return ImportResult{}, err
	}

	now := time.Now().UTC().UnixMilli()
	bookID, err := db.upsertBook(ctx, tx, existingID, path, title, now)
	if err != nil {
		return ImportResult{}, err
	}
	if err := db.replaceChunks(ctx, tx, seg, bookID, chunks); err != nil {
		return ImportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ImportResult{}, fmt.Errorf("commit import: %w", err)
	}
	return ImportResult{
		BookID:       bookID,
		Title:        title,
		ParentChunks: len(chunks.Parents),
		Chunks:       len(chunks.Children),
		Indexed:      true,
	}, nil
}

func findExistingBook(ctx context.Context, tx *loggedTx, sourcePath string) (int64, error) {
	var id int64
	err := tx.queryRow(ctx, `
		SELECT id FROM books
		WHERE source_path = ? AND active = 1
	`, sourcePath).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return 0, fmt.Errorf("find existing book: %w", err)
}

func (db *DB) upsertBook(ctx context.Context, tx *loggedTx, existingID int64, sourcePath, title string, now int64) (int64, error) {
	if existingID > 0 {
		_, err := tx.exec(ctx, `
			UPDATE books
			SET title = ?, modified_at = ?, active = 1
			WHERE id = ?
		`, title, now, existingID)
		if err != nil {
			return 0, fmt.Errorf("update book: %w", err)
		}
		return existingID, nil
	}
	bookID := db.ids.Next()
	_, err := tx.exec(ctx, `
		INSERT INTO books(id, source_path, title, created_at, modified_at, active)
		VALUES (?, ?, ?, ?, ?, 1)
	`, bookID, sourcePath, title, now, now)
	if err != nil {
		return 0, fmt.Errorf("insert book: %w", err)
	}
	return bookID, nil
}

func (db *DB) replaceChunks(ctx context.Context, tx *loggedTx, seg *segment.Segmenter, bookID int64, chunks chunkedMarkdown) error {
	if _, err := tx.exec(ctx, `
		DELETE FROM chunks_fts
		WHERE rowid IN (SELECT id FROM chunks WHERE book_id = ?)
	`, bookID); err != nil {
		return fmt.Errorf("delete old fts rows: %w", err)
	}
	if err := deleteVectorRowsForBook(ctx, tx, bookID); err != nil {
		return err
	}
	if _, err := tx.exec(ctx, `
		DELETE FROM embeddings
		WHERE chunk_id IN (SELECT id FROM chunks WHERE book_id = ?)
	`, bookID); err != nil {
		return fmt.Errorf("delete old embeddings: %w", err)
	}
	if _, err := tx.exec(ctx, `DELETE FROM chunks WHERE book_id = ?`, bookID); err != nil {
		return fmt.Errorf("delete old chunks: %w", err)
	}
	if _, err := tx.exec(ctx, `DELETE FROM parent_chunks WHERE book_id = ?`, bookID); err != nil {
		return fmt.Errorf("delete old parent chunks: %w", err)
	}

	insertParent, err := tx.prepare(ctx, `
		INSERT INTO parent_chunks(id, book_id, title, heading_path, text)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert parent chunk: %w", err)
	}
	defer insertParent.Close()

	insertChunk, err := tx.prepare(ctx, `
		INSERT INTO chunks(id, book_id, parent_chunk_id, title, heading_path, text)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert chunk: %w", err)
	}
	defer insertChunk.Close()

	insertFTS, err := tx.prepare(ctx, `
		INSERT INTO chunks_fts(rowid, title_bm25, heading_bm25, text_bm25)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert fts: %w", err)
	}
	defer insertFTS.Close()

	parentIDs := make([]int64, len(chunks.Parents))
	for i := range chunks.Parents {
		parent := chunks.Parents[i]
		parentID := db.ids.Next()
		parentIDs[i] = parentID
		_, err := insertParent.exec(ctx,
			parentID, bookID, parent.Title, parent.HeadingPath, parent.Text,
		)
		if err != nil {
			return fmt.Errorf("insert parent chunk id %d: %w", parentID, err)
		}
	}

	for i := range chunks.Children {
		child := chunks.Children[i]
		chunk := child.Chunk
		chunkID := db.ids.Next()
		parentID := parentIDs[child.ParentIndex]
		_, err := insertChunk.exec(ctx,
			chunkID, bookID, parentID, chunk.Title, chunk.HeadingPath, chunk.Text,
		)
		if err != nil {
			return fmt.Errorf("insert chunk id %d: %w", chunkID, err)
		}
		_, err = insertFTS.exec(ctx,
			chunkID,
			seg.IndexText(chunk.Title),
			seg.IndexText(chunk.HeadingPath),
			seg.IndexText(chunk.Text),
		)
		if err != nil {
			return fmt.Errorf("insert fts chunk id %d: %w", chunkID, err)
		}
	}
	return nil
}

func extractTitle(content, path string) string {
	if strings.HasPrefix(content, "---\n") {
		if end := strings.Index(content[4:], "\n---"); end >= 0 {
			front := content[4 : 4+end]
			for _, line := range strings.Split(front, "\n") {
				key, value, ok := strings.Cut(line, ":")
				if ok && strings.EqualFold(strings.TrimSpace(key), "title") {
					value = strings.Trim(strings.TrimSpace(value), `"'`)
					if value != "" {
						return value
					}
				}
			}
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if match := headingRE.FindStringSubmatch(line); match != nil {
			return strings.TrimSpace(match[2])
		}
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func rollback(tx interface{ Rollback() error }) {
	_ = tx.Rollback()
}
