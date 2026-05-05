package store

import (
	"context"
	"fmt"
)

const (
	vectorDimension = 1024
)

func saveVectorIndexTx(ctx context.Context, tx *loggedTx, chunkID int64, bookID int64, dim int, blob []byte) error {
	if dim != vectorDimension {
		return fmt.Errorf("embedding dimension = %d, want %d", dim, vectorDimension)
	}
	if err := deleteVectorRowTx(ctx, tx, chunkID); err != nil {
		return err
	}
	_, err := tx.exec(ctx, `
		INSERT INTO chunk_vectors(rowid, embedding, book_id)
		VALUES (?, ?, ?)
	`, chunkID, blob, bookID)
	if err != nil {
		return fmt.Errorf("insert vector index row: %w", err)
	}
	return nil
}

func chunkBookIDTx(ctx context.Context, tx *loggedTx, chunkID int64) (int64, error) {
	var bookID int64
	if err := tx.queryRow(ctx, `SELECT book_id FROM chunks WHERE id = ?`, chunkID).Scan(&bookID); err != nil {
		return 0, fmt.Errorf("read chunk book id: %w", err)
	}
	return bookID, nil
}

func deleteVectorRowsForBook(ctx context.Context, tx *loggedTx, bookID int64) error {
	_, err := tx.exec(ctx, `
		DELETE FROM chunk_vectors
		WHERE rowid IN (SELECT id FROM chunks WHERE book_id = ?)
	`, bookID)
	if err != nil {
		return fmt.Errorf("delete old vector index rows: %w", err)
	}
	return nil
}

func deleteVectorRowTx(ctx context.Context, tx *loggedTx, chunkID int64) error {
	_, err := tx.exec(ctx, `DELETE FROM chunk_vectors WHERE rowid = ?`, chunkID)
	if err != nil {
		return fmt.Errorf("delete stale vector index row: %w", err)
	}
	return nil
}
