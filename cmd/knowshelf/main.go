package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.Default()
	root := newRootCommand(ctx, logger)
	if err := root.ExecuteContext(ctx); err != nil {
		logger.LogAttrs(root.Context(), slog.LevelError, "command failed",
			slog.String("error", err.Error()))
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCommand(ctx context.Context, logger *slog.Logger) *cobra.Command {
	var configPath string
	root := &cobra.Command{
		Use:           "knowshelf",
		Short:         "Personal markdown book knowledge base",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetContext(ctx)
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "", "YAML config path")
	root.AddCommand(
		newRunCommand(&configPath, logger),
		newImportCommand(&configPath, logger),
		newEmbedCommand(&configPath, logger),
		newSearchCommand(&configPath, logger),
		newGetCommand(&configPath, logger),
		newTokenGenCommand(&configPath),
		newVersionCommand(),
	)
	return root
}
