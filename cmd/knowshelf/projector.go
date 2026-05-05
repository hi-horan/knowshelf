package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"knowshelf/internal/store"

	"github.com/spf13/cobra"
)

const defaultProjectorOut = "_output/projector"

func newEmbedExportCommand(configPath *string, logger *slog.Logger) *cobra.Command {
	var outDir string
	var limit int
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export vectors for TensorFlow Embedding Projector",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStore(cmd.Context(), logger, *configPath, "embed.export", func(ctx context.Context, db *store.DB) error {
				app := &application{db: db}
				return app.exportProjector(ctx, outDir, limit, cmd.OutOrStdout())
			})
		},
	}
	cmd.Flags().StringVar(&outDir, "out", defaultProjectorOut, "output directory for projector TSV files")
	cmd.Flags().IntVar(&limit, "limit", 0, "max vector rows to export; 0 means all")
	return cmd
}

func (a *application) exportProjector(ctx context.Context, outDir string, limit int, out io.Writer) error {
	if strings.TrimSpace(outDir) == "" {
		return errors.New("output directory is empty")
	}
	if limit < 0 {
		return errors.New("limit must be non-negative")
	}
	rows, err := a.db.ListVectorRows(ctx, limit)
	if err != nil {
		return err
	}
	if err := writeProjectorFiles(outDir, rows); err != nil {
		return err
	}
	fmt.Fprintf(out, "exported projector vectors=%d out=%s\n", len(rows), outDir)
	return nil
}

func writeProjectorFiles(outDir string, rows []store.VectorRow) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create projector output dir: %w", err)
	}
	if err := writeProjectorVectors(filepath.Join(outDir, "vectors.tsv"), rows); err != nil {
		return err
	}
	if err := writeProjectorMetadata(filepath.Join(outDir, "metadata.tsv"), rows); err != nil {
		return err
	}
	return nil
}

func writeProjectorVectors(path string, rows []store.VectorRow) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create vectors.tsv: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close vectors.tsv: %w", closeErr)
		}
	}()

	writer := bufio.NewWriter(file)
	for _, row := range rows {
		for i, value := range row.Embedding {
			if i > 0 {
				if _, err := writer.WriteString("\t"); err != nil {
					return fmt.Errorf("write vectors.tsv: %w", err)
				}
			}
			if _, err := writer.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32)); err != nil {
				return fmt.Errorf("write vectors.tsv: %w", err)
			}
		}
		if _, err := writer.WriteString("\n"); err != nil {
			return fmt.Errorf("write vectors.tsv: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush vectors.tsv: %w", err)
	}
	return nil
}

func writeProjectorMetadata(path string, rows []store.VectorRow) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create metadata.tsv: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close metadata.tsv: %w", closeErr)
		}
	}()

	writer := bufio.NewWriter(file)
	if _, err := writer.WriteString("rowid\tchunk_id\tbook_id\tbook_title\ttitle\theading_path\ttext\n"); err != nil {
		return fmt.Errorf("write metadata.tsv header: %w", err)
	}
	for _, row := range rows {
		fields := []string{
			strconv.FormatInt(row.RowID, 10),
			strconv.FormatInt(row.ChunkID, 10),
			strconv.FormatInt(row.BookID, 10),
			sanitizeProjectorMetadata(row.BookTitle),
			sanitizeProjectorMetadata(row.Title),
			sanitizeProjectorMetadata(row.HeadingPath),
			sanitizeProjectorMetadata(row.Text),
		}
		if _, err := writer.WriteString(strings.Join(fields, "\t") + "\n"); err != nil {
			return fmt.Errorf("write metadata.tsv: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush metadata.tsv: %w", err)
	}
	return nil
}

func sanitizeProjectorMetadata(value string) string {
	replacer := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	return replacer.Replace(value)
}
