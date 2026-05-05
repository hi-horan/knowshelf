package store

import (
	"context"
	"database/sql"
	"fmt"
)

// GetBook 按 id 返回书籍。
func (db *DB) GetBook(ctx context.Context, id int64) (Book, error) {
	var b Book
	var createdAt, modifiedAt int64
	var active int
	err := db.queryRow(ctx, `
		SELECT id, source_path, title, created_at, modified_at, active
		FROM books WHERE id = ? AND active = 1
	`, id).Scan(&b.ID, &b.SourcePath, &b.Title, &createdAt, &modifiedAt, &active)
	if err != nil {
		return Book{}, fmt.Errorf("get book: %w", err)
	}
	b.CreatedAt = timeFromUnixMilli(createdAt)
	b.ModifiedAt = timeFromUnixMilli(modifiedAt)
	b.Active = active == 1
	return b, nil
}

// ListBooks 返回所有 active 书籍。
func (db *DB) ListBooks(ctx context.Context) ([]Book, error) {
	rows, err := db.query(ctx, `
		SELECT id, title
		FROM books
		WHERE active = 1
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var book Book
		if err := rows.Scan(&book.ID, &book.Title); err != nil {
			return nil, fmt.Errorf("scan book: %w", err)
		}
		book.Active = true
		books = append(books, book)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate books: %w", err)
	}
	return books, nil
}

// GetChunk 按 id 返回 chunk。
func (db *DB) GetChunk(ctx context.Context, id int64) (Chunk, Book, error) {
	row := db.queryRow(ctx, `
		SELECT
			c.id, c.book_id, c.parent_chunk_id, c.title, c.heading_path, c.text,
			b.id, b.source_path, b.title,
			b.created_at, b.modified_at, b.active
		FROM chunks c
		JOIN books b ON b.id = c.book_id
		WHERE c.id = ? AND b.active = 1
	`, id)
	return scanChunkBook(row)
}

// GetParentChunk 按 id 返回 parent chunk。
func (db *DB) GetParentChunk(ctx context.Context, id int64) (ParentChunk, Book, error) {
	row := db.queryRow(ctx, `
		SELECT
			p.id, p.book_id, p.title, p.heading_path, p.text,
			b.id, b.source_path, b.title,
			b.created_at, b.modified_at, b.active
		FROM parent_chunks p
		JOIN books b ON b.id = p.book_id
		WHERE p.id = ? AND b.active = 1
	`, id)
	return scanParentChunkBook(row)
}

// FindBookByPath 按精确源路径返回书籍。
func (db *DB) FindBookByPath(ctx context.Context, path string) (Book, error) {
	var b Book
	var createdAt, modifiedAt int64
	var active int
	err := db.queryRow(ctx, `
		SELECT id, source_path, title, created_at, modified_at, active
		FROM books WHERE source_path = ? AND active = 1
	`, path).Scan(&b.ID, &b.SourcePath, &b.Title, &createdAt, &modifiedAt, &active)
	if err != nil {
		return Book{}, fmt.Errorf("find book by path: %w", err)
	}
	b.CreatedAt = timeFromUnixMilli(createdAt)
	b.ModifiedAt = timeFromUnixMilli(modifiedAt)
	b.Active = active == 1
	return b, nil
}

func scanChunkBook(row interface {
	Scan(dest ...any) error
}) (Chunk, Book, error) {
	var c Chunk
	var b Book
	var createdAt, modifiedAt int64
	var active int
	err := row.Scan(
		&c.ID, &c.BookID, &c.ParentChunkID, &c.Title, &c.HeadingPath, &c.Text,
		&b.ID, &b.SourcePath, &b.Title,
		&createdAt, &modifiedAt, &active,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Chunk{}, Book{}, err
		}
		return Chunk{}, Book{}, fmt.Errorf("scan chunk/book: %w", err)
	}
	b.CreatedAt = timeFromUnixMilli(createdAt)
	b.ModifiedAt = timeFromUnixMilli(modifiedAt)
	b.Active = active == 1
	return c, b, nil
}

func scanParentChunkBook(row interface {
	Scan(dest ...any) error
}) (ParentChunk, Book, error) {
	var p ParentChunk
	var b Book
	var createdAt, modifiedAt int64
	var active int
	err := row.Scan(
		&p.ID, &p.BookID, &p.Title, &p.HeadingPath, &p.Text,
		&b.ID, &b.SourcePath, &b.Title,
		&createdAt, &modifiedAt, &active,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return ParentChunk{}, Book{}, err
		}
		return ParentChunk{}, Book{}, fmt.Errorf("scan parent chunk/book: %w", err)
	}
	b.CreatedAt = timeFromUnixMilli(createdAt)
	b.ModifiedAt = timeFromUnixMilli(modifiedAt)
	b.Active = active == 1
	return p, b, nil
}

// Status 返回索引状态。
func (db *DB) Status(ctx context.Context) (Status, error) {
	var status Status
	if err := db.queryRow(ctx, `SELECT COUNT(*) FROM books WHERE active = 1`).Scan(&status.Books); err != nil {
		return Status{}, fmt.Errorf("count books: %w", err)
	}
	if err := db.queryRow(ctx, `
		SELECT COUNT(*) FROM chunks c JOIN books b ON b.id = c.book_id WHERE b.active = 1
	`).Scan(&status.Chunks); err != nil {
		return Status{}, fmt.Errorf("count chunks: %w", err)
	}
	if err := db.queryRow(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&status.Embeddings); err != nil {
		return Status{}, fmt.Errorf("count embeddings: %w", err)
	}
	if err := db.queryRow(ctx, `
		SELECT COUNT(*)
		FROM chunks c
		JOIN books b ON b.id = c.book_id
		LEFT JOIN embeddings e ON e.chunk_id = c.id
		WHERE b.active = 1 AND e.chunk_id IS NULL
	`).Scan(&status.ChunksToEmbed); err != nil {
		return Status{}, fmt.Errorf("count pending embeddings: %w", err)
	}
	return status, nil
}
