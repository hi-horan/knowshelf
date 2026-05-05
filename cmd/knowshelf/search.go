package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"knowshelf/internal/search"

	"github.com/spf13/cobra"
)

func newSearchCommand(configPath *string, logger *slog.Logger) *cobra.Command {
	var limit int
	var jsonOut bool
	var noRerank bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search imported books",
		Long:  "Search imported books with the single hybrid retrieval pipeline.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withApp(cmd.Context(), logger, *configPath, "search", func(ctx context.Context, app *application) error {
				return app.searchBooks(ctx, strings.Join(args, " "), searchOptions{
					limit:    limit,
					jsonOut:  jsonOut,
					noRerank: noRerank,
				})
			})
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "result limit")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	cmd.Flags().BoolVar(&noRerank, "no-rerank", false, "skip reranking")
	return cmd
}

type searchOptions struct {
	limit    int
	jsonOut  bool
	noRerank bool
}

func (a *application) searchBooks(ctx context.Context, query string, opts searchOptions) error {
	results, err := a.search.Search(ctx, search.Options{
		Question:      query,
		Limit:         opts.limit,
		DisableRerank: opts.noRerank,
	})
	if err != nil {
		return err
	}
	if opts.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	for _, result := range results {
		fmt.Printf("#child:%d parent:%d book:%d %.3f %s %s\n%s\n\n",
			result.ChunkID, result.ParentChunkID, result.BookID, result.Score, result.BookTitle,
			result.HeadingPath, result.Text)
	}
	return nil
}
