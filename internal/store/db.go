package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"knowshelf/internal/pkg/idgen"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

// DB 封装 SQLite 连接。
type DB struct {
	sql    *sql.DB
	ids    *idgen.Generator
	logger *slog.Logger
}

// Option 配置 DB。
type Option func(*DB) error

// WithLogger 配置 DB 使用显式 logger。
func WithLogger(logger *slog.Logger) Option {
	return func(db *DB) error {
		if logger != nil {
			db.logger = logger
		}
		return nil
	}
}

// Open 打开 SQLite 数据库并确保 schema 已创建。
func Open(ctx context.Context, path string, opts ...Option) (*DB, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database dir: %w", err)
	}
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	ids := idgen.New()
	db := &DB{sql: sqlDB, ids: ids, logger: slog.Default()}
	for _, opt := range opts {
		if err := opt(db); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
	}
	if err := db.init(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Close 关闭数据库。
func (db *DB) Close() error {
	return db.sql.Close()
}

func (db *DB) log() *slog.Logger {
	if db.logger != nil {
		return db.logger
	}
	return slog.Default()
}

func (db *DB) init(ctx context.Context) error {
	if err := execSchemaSQL(ctx, db.log(), db.sql); err != nil {
		return err
	}
	return nil
}

func timeFromUnixMilli(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}
