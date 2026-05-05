package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"knowshelf/internal/models"
	"knowshelf/internal/store"

	"github.com/spf13/cobra"
)

const embedBatchSize = 10

func newEmbedCommand(configPath *string, logger *slog.Logger) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "embed",
		Short: "Generate embeddings for pending chunks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withApp(cmd.Context(), logger, *configPath, "embed", func(ctx context.Context, app *application) error {
				return app.embedChunks(ctx, limit)
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "max chunks to embed; 0 means all pending chunks")
	cmd.AddCommand(
		newEmbedShowCommand(configPath, logger),
		newEmbedExportCommand(configPath, logger),
	)
	return cmd
}

func newEmbedShowCommand(configPath *string, logger *slog.Logger) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show rows from chunk_vectors",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStore(cmd.Context(), logger, *configPath, "embed.show", func(ctx context.Context, db *store.DB) error {
				app := &application{db: db}
				return app.showVectorRows(ctx, limit, cmd.OutOrStdout())
			})
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 1, "max vector rows to show")
	return cmd
}

func (a *application) embedChunks(ctx context.Context, limit int) error {
	if a.embedder == nil {
		return errors.New("embedding model is not configured")
	}
	remaining := limit
	total := 0
	for {
		batchLimit := embedBatchSize
		if remaining > 0 && remaining < batchLimit {
			batchLimit = remaining
		}
		chunks, err := a.db.PendingEmbeddingChunks(ctx, batchLimit)
		if err != nil {
			return err
		}
		if len(chunks) == 0 {
			break
		}
		inputs := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			inputs = append(inputs, a.embedder.FormatDocument(chunk.Title, chunk.Text))
		}
		vectors, err := a.embedder.EmbedStrings(ctx, inputs)
		if err != nil {
			return err
		}
		for i, vector := range vectors {
			if i >= len(chunks) || len(vector) == 0 {
				continue
			}
			if err := a.db.SaveEmbedding(ctx, chunks[i].ID, models.Float64ToFloat32(vector)); err != nil {
				return err
			}
			total++
		}
		if remaining > 0 {
			remaining -= len(chunks)
			if remaining <= 0 {
				break
			}
		}
	}
	fmt.Printf("embedded chunks=%d\n", total)
	return nil
}

func (a *application) showVectorRows(ctx context.Context, limit int, out io.Writer) error {
	if limit <= 0 {
		return errors.New("limit must be positive")
	}
	rows, err := a.db.ListVectorRows(ctx, limit)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}
