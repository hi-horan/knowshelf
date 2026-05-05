package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"knowshelf/internal/store"

	"github.com/spf13/cobra"
)

func newImportCommand(configPath *string, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <book.md>",
		Short: "Import a markdown book",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withApp(cmd.Context(), logger, *configPath, "import", func(ctx context.Context, app *application) error {
				return app.importBook(ctx, args[0])
			})
		},
	}
	return cmd
}

func (a *application) importBook(ctx context.Context, path string) error {
	name, err := copyMarkdownToLibrary(path, a.markdownDir)
	if err != nil {
		return err
	}
	result, err := a.importMarkdownFromLibrary(ctx, name)
	if err != nil {
		return err
	}
	fmt.Printf("imported book=%d title=%q parents=%d chunks=%d\n", result.BookID, result.Title, result.ParentChunks, result.Chunks)
	return nil
}

func (a *application) importMarkdownFromLibrary(ctx context.Context, name string) (store.ImportResult, error) {
	return a.db.ImportMarkdown(ctx, a.segmenter, store.ImportOptions{
		Name:        name,
		MarkdownDir: a.markdownDir,
	})
}

func copyMarkdownToLibrary(sourcePath, markdownDir string) (string, error) {
	name := filepath.Base(sourcePath)
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("read source markdown: %w", err)
	}
	if err := os.MkdirAll(markdownDir, 0o755); err != nil {
		return "", fmt.Errorf("create markdown dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(markdownDir, name), content, 0o644); err != nil {
		return "", fmt.Errorf("copy markdown to library: %w", err)
	}
	return name, nil
}
