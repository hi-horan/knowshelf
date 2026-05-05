package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"knowshelf/internal/config"
	"knowshelf/internal/models"
	"knowshelf/internal/observability"
	"knowshelf/internal/search"
	"knowshelf/internal/segment"
	"knowshelf/internal/store"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const traceShutdownTimeout = 5 * time.Second

type appCloser struct {
	name  string
	close func() error
}

type application struct {
	cfg         *config.Config
	logger      *slog.Logger
	markdownDir string
	db          *store.DB
	segmenter   *segment.Segmenter
	search      *search.Service
	embedder    models.Embedder
	closers     []appCloser
}

func openApp(ctx context.Context, cfg *config.Config, logger *slog.Logger) (app *application, err error) {
	app = &application{
		cfg:    cfg,
		logger: logger,
	}
	defer func() {
		if err != nil {
			if app != nil {
				app.close(ctx)
			}
			app = nil
		}
	}()

	app.cfg = cfg
	app.markdownDir = cfg.Library.MarkdownDir
	db, err := store.Open(ctx, cfg.Database.Path, store.WithLogger(logger))
	if err != nil {
		return nil, err
	}
	app.db = db
	app.addCloser("database", db.Close)

	seg, err := segment.New(cfg.Segment.Dictionaries)
	if err != nil {
		return nil, fmt.Errorf("create segmenter: %w", err)
	}
	app.segmenter = seg

	var embedder models.Embedder
	if cfg.Search.EnableVector {
		embedder, err = models.NewEmbedder(ctx, &cfg.Models.Embedding)
		if err != nil {
			return nil, err
		}
	}
	app.embedder = embedder

	var reranker models.Reranker
	if cfg.Search.EnableRerank {
		reranker, err = models.NewOpenAIReranker(&cfg.Models.Rerank, models.WithLogger(logger))
		if err != nil {
			return nil, err
		}
	}
	service, err := search.NewService(db, seg, cfg.Search,
		search.WithLogger(logger),
		search.WithEmbedder(embedder),
		search.WithReranker(reranker),
	)
	if err != nil {
		return nil, err
	}
	app.search = service
	return app, nil
}

func withApp(ctx context.Context, fallbackLogger *slog.Logger, configPath string, operation string, fn func(context.Context, *application) error) (err error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fallbackLogger.LogAttrs(ctx, slog.LevelError, "load config failed",
			slog.String("error", err.Error()))
		return err
	}
	logger := config.NewLogger(cfg)
	shutdownTracing, err := observability.ConfigureTracing(ctx, cfg)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "configure tracing failed",
			slog.String("error", err.Error()))
		return err
	}

	tracer := otel.Tracer("knowshelf/cmd")
	ctx, span := tracer.Start(ctx, "knowshelf."+operation,
		trace.WithAttributes(attribute.String("knowshelf.command", operation)))
	defer func() {
		shutdownTraceProvider(ctx, logger, shutdownTracing)
	}()
	defer finishSpan(span, &err)

	startAttrs := []slog.Attr{slog.String("command", operation)}
	if operation == "run" {
		startAttrs = append(startAttrs, versionInfoAttrs(currentVersionInfo())...)
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "command started", startAttrs...)
	app, err := openApp(ctx, cfg, logger)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "open application failed",
			slog.String("command", operation),
			slog.String("error", err.Error()))
		return err
	}
	defer app.close(ctx)
	if err = fn(ctx, app); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "command failed",
			slog.String("command", operation),
			slog.String("error", err.Error()))
		return err
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "command finished",
		slog.String("command", operation))
	return nil
}

func withStore(ctx context.Context, fallbackLogger *slog.Logger, configPath string, operation string, fn func(context.Context, *store.DB) error) (err error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fallbackLogger.LogAttrs(ctx, slog.LevelError, "load config failed",
			slog.String("error", err.Error()))
		return err
	}
	logger := config.NewLogger(cfg)
	shutdownTracing, err := observability.ConfigureTracing(ctx, cfg)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "configure tracing failed",
			slog.String("error", err.Error()))
		return err
	}

	tracer := otel.Tracer("knowshelf/cmd")
	ctx, span := tracer.Start(ctx, "knowshelf."+operation,
		trace.WithAttributes(attribute.String("knowshelf.command", operation)))
	defer func() {
		shutdownTraceProvider(ctx, logger, shutdownTracing)
	}()
	defer finishSpan(span, &err)

	logger.LogAttrs(ctx, slog.LevelInfo, "command started",
		slog.String("command", operation))
	db, err := store.Open(ctx, cfg.Database.Path, store.WithLogger(logger))
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "open database failed",
			slog.String("command", operation),
			slog.String("error", err.Error()))
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			logger.LogAttrs(ctx, slog.LevelWarn, "close resource failed",
				slog.String("resource", "database"),
				slog.String("error", closeErr.Error()))
		}
	}()
	if err = fn(ctx, db); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "command failed",
			slog.String("command", operation),
			slog.String("error", err.Error()))
		return err
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "command finished",
		slog.String("command", operation))
	return nil
}

func finishSpan(span trace.Span, err *error) {
	if err != nil && *err != nil {
		span.RecordError(*err)
		span.SetStatus(codes.Error, (*err).Error())
	}
	span.End()
}

func shutdownTraceProvider(parent context.Context, logger *slog.Logger, shutdown observability.ShutdownFunc) {
	if shutdown == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), traceShutdownTimeout)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		logger.LogAttrs(parent, slog.LevelWarn, "shutdown tracing failed",
			slog.String("error", err.Error()))
	}
}

func (a *application) addCloser(name string, close func() error) {
	a.closers = append(a.closers, appCloser{name: name, close: close})
}

func (a *application) close(ctx context.Context) {
	for i := len(a.closers) - 1; i >= 0; i-- {
		closer := a.closers[i]
		if err := closer.close(); err != nil {
			a.log().LogAttrs(ctx, slog.LevelWarn, "close resource failed",
				slog.String("resource", closer.name),
				slog.String("error", err.Error()))
		}
	}
	a.closers = nil
}

func (a *application) log() *slog.Logger {
	if a.logger != nil {
		return a.logger
	}
	return slog.Default()
}
