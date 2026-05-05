package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"knowshelf/internal/mcp"

	"github.com/spf13/cobra"
)

const (
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpWriteTimeout      = 5 * time.Minute
	httpIdleTimeout       = 2 * time.Minute
)

func newRunCommand(configPath *string, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the MCP Streamable HTTP service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withApp(cmd.Context(), logger, *configPath, "run", func(ctx context.Context, app *application) error {
				return app.runService(ctx)
			})
		},
	}
	return cmd
}

func (a *application) runService(ctx context.Context) error {
	a.log().Log(ctx, slog.LevelDebug, "print config", slog.Any("cfg", a.cfg))
	mcpServer, err := mcp.NewServer(a.db, a.search, a.segmenter, a.cfg, mcp.WithLogger(a.log()))
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle(a.cfg.MCP.Path, mcpServer.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	httpServer := &http.Server{
		Addr:              a.cfg.MCP.Address,
		Handler:           mux,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
	}
	listener, err := net.Listen("tcp", a.cfg.MCP.Address)
	if err != nil {
		return fmt.Errorf("listen mcp http: %w", err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.Serve(listener)
	}()
	a.log().LogAttrs(ctx, slog.LevelInfo, "mcp http server started",
		slog.String("address", listener.Addr().String()),
		slog.String("path", a.cfg.MCP.Path))

	select {
	case <-ctx.Done():
		a.log().LogAttrs(ctx, slog.LevelInfo, "mcp http server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), traceShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = httpServer.Close()
			return fmt.Errorf("shutdown mcp http: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve mcp http: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve mcp http: %w", err)
	}
}
