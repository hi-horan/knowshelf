package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
)

//go:embed sql/schema.sql
var embeddedSchemaSQL []byte

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func execSchemaSQL(ctx context.Context, logger *slog.Logger, execer sqlExecutor) error {
	sqlText := string(embeddedSchemaSQL)
	if _, err := execWithSQLLog(ctx, logger, execer, "exec", sqlText); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}
