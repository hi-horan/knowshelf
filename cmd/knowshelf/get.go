package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"knowshelf/internal/store"

	"github.com/spf13/cobra"
)

func newGetCommand(configPath *string, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <chunk-id|chunk:id|parent:id|book:id>",
		Short: "Get a child chunk, parent chunk, or whole book",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withApp(cmd.Context(), logger, *configPath, "get", func(ctx context.Context, app *application) error {
				return app.getContent(ctx, args[0])
			})
		},
	}
	return cmd
}

func (a *application) getContent(ctx context.Context, target string) error {
	if strings.HasPrefix(target, "book:") {
		id, err := strconv.ParseInt(strings.TrimPrefix(target, "book:"), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid book id: %w", err)
		}
		book, err := a.db.GetBook(ctx, id)
		if err != nil {
			return err
		}
		content, err := store.ReadMarkdownFile(book.SourcePath)
		if err != nil {
			return err
		}
		fmt.Print(content)
		return nil
	}
	if strings.HasPrefix(target, "parent:") {
		id, err := strconv.ParseInt(strings.TrimPrefix(target, "parent:"), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid parent chunk id: %w", err)
		}
		parent, _, err := a.db.GetParentChunk(ctx, id)
		if err != nil {
			return err
		}
		fmt.Print(parent.Text)
		return nil
	}
	target = strings.TrimPrefix(target, "chunk:")
	id, err := strconv.ParseInt(target, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chunk id: %w", err)
	}
	chunk, _, err := a.db.GetChunk(ctx, id)
	if err != nil {
		return err
	}
	fmt.Print(chunk.Text)
	return nil
}
