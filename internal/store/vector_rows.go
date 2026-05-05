package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
)

// ListVectorRows 返回 chunk_vectors 中的向量记录；limit <= 0 表示全部。
func (db *DB) ListVectorRows(ctx context.Context, limit int) ([]VectorRow, error) {
	sqlText := `
		SELECT
			v.rowid, c.id, v.book_id, b.title, c.title, c.heading_path, c.text, v.embedding
		FROM chunk_vectors v
		LEFT JOIN chunks c ON c.id = v.rowid
		LEFT JOIN books b ON b.id = v.book_id
		ORDER BY v.rowid DESC
	`
	args := []any{}
	if limit > 0 {
		sqlText += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("list vector rows: %w", err)
	}
	defer rows.Close()

	var results []VectorRow
	for rows.Next() {
		row, err := scanVectorRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vector rows: %w", err)
	}
	return results, nil
}

func scanVectorRow(rows interface {
	Scan(dest ...any) error
}) (VectorRow, error) {
	var row VectorRow
	var chunkID sql.NullInt64
	var bookID sql.NullInt64
	var bookTitle sql.NullString
	var title sql.NullString
	var headingPath sql.NullString
	var text sql.NullString
	var blob []byte
	if err := rows.Scan(
		&row.RowID,
		&chunkID,
		&bookID,
		&bookTitle,
		&title,
		&headingPath,
		&text,
		&blob,
	); err != nil {
		return VectorRow{}, fmt.Errorf("scan vector row: %w", err)
	}
	row.ChunkID = chunkID.Int64
	row.BookID = bookID.Int64
	row.BookTitle = bookTitle.String
	row.Title = title.String
	row.HeadingPath = headingPath.String
	row.Text = text.String

	vector, err := decodeVector(blob)
	if err != nil {
		return VectorRow{}, err
	}
	row.Embedding = vector
	row.EmbeddingDim = len(vector)
	return row, nil
}

func decodeVector(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 {
		return nil, fmt.Errorf("decode vector: blob length %d is not a float32 vector", len(blob))
	}
	vector := make([]float32, len(blob)/4)
	for i := range vector {
		bits := binary.LittleEndian.Uint32(blob[i*4 : (i+1)*4])
		vector[i] = math.Float32frombits(bits)
	}
	return vector, nil
}
