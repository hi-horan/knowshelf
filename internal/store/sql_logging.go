package store

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"
)

type sqlQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type loggedTx struct {
	tx     *sql.Tx
	logger *slog.Logger
}

type loggedStmt struct {
	stmt    *sql.Stmt
	sqlText string
	logger  *slog.Logger
}

type loggedRow struct {
	ctx       context.Context
	row       *sql.Row
	sqlText   string
	argCount  int
	startedAt time.Time
	operation string
	logger    *slog.Logger
}

func (db *DB) beginTx(ctx context.Context, opts *sql.TxOptions) (*loggedTx, error) {
	tx, err := db.sql.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &loggedTx{tx: tx, logger: db.log()}, nil
}

func (db *DB) query(ctx context.Context, sqlText string, args ...any) (*sql.Rows, error) {
	return queryWithSQLLog(ctx, db.log(), db.sql, "query", sqlText, args...)
}

func (db *DB) queryRow(ctx context.Context, sqlText string, args ...any) *loggedRow {
	startedAt := time.Now()
	return &loggedRow{
		ctx:       ctx,
		row:       db.sql.QueryRowContext(ctx, sqlText, args...),
		sqlText:   sqlText,
		argCount:  len(args),
		startedAt: startedAt,
		operation: "query_row",
		logger:    db.log(),
	}
}

func (tx *loggedTx) Commit() error {
	return tx.tx.Commit()
}

func (tx *loggedTx) Rollback() error {
	return tx.tx.Rollback()
}

func (tx *loggedTx) exec(ctx context.Context, sqlText string, args ...any) (sql.Result, error) {
	return execWithSQLLog(ctx, tx.logger, tx.tx, "tx_exec", sqlText, args...)
}

func (tx *loggedTx) query(ctx context.Context, sqlText string, args ...any) (*sql.Rows, error) {
	return queryWithSQLLog(ctx, tx.logger, tx.tx, "tx_query", sqlText, args...)
}

func (tx *loggedTx) queryRow(ctx context.Context, sqlText string, args ...any) *loggedRow {
	startedAt := time.Now()
	return &loggedRow{
		ctx:       ctx,
		row:       tx.tx.QueryRowContext(ctx, sqlText, args...),
		sqlText:   sqlText,
		argCount:  len(args),
		startedAt: startedAt,
		operation: "tx_query_row",
		logger:    tx.logger,
	}
}

func (tx *loggedTx) prepare(ctx context.Context, sqlText string) (*loggedStmt, error) {
	startedAt := time.Now()
	stmt, err := tx.tx.PrepareContext(ctx, sqlText)
	logSQL(ctx, tx.logger, "tx_prepare", sqlText, 0, time.Since(startedAt), err)
	if err != nil {
		return nil, err
	}
	return &loggedStmt{stmt: stmt, sqlText: sqlText, logger: tx.logger}, nil
}

func (stmt *loggedStmt) Close() error {
	return stmt.stmt.Close()
}

func (stmt *loggedStmt) exec(ctx context.Context, args ...any) (sql.Result, error) {
	startedAt := time.Now()
	result, err := stmt.stmt.ExecContext(ctx, args...)
	logSQL(ctx, stmt.logger, "stmt_exec", stmt.sqlText, len(args), time.Since(startedAt), err)
	return result, err
}

func (row *loggedRow) Scan(dest ...any) error {
	err := row.row.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		logSQL(row.ctx, row.logger, row.operation, row.sqlText, row.argCount, time.Since(row.startedAt), nil, slog.Bool("row_found", false))
		return err
	}
	logSQL(row.ctx, row.logger, row.operation, row.sqlText, row.argCount, time.Since(row.startedAt), err)
	return err
}

func execWithSQLLog(ctx context.Context, logger *slog.Logger, execer sqlExecutor, operation string, sqlText string, args ...any) (sql.Result, error) {
	startedAt := time.Now()
	result, err := execer.ExecContext(ctx, sqlText, args...)
	logSQL(ctx, logger, operation, sqlText, len(args), time.Since(startedAt), err)
	return result, err
}

func queryWithSQLLog(ctx context.Context, logger *slog.Logger, queryer sqlQueryer, operation string, sqlText string, args ...any) (*sql.Rows, error) {
	startedAt := time.Now()
	rows, err := queryer.QueryContext(ctx, sqlText, args...)
	logSQL(ctx, logger, operation, sqlText, len(args), time.Since(startedAt), err)
	return rows, err
}

func logSQL(ctx context.Context, logger *slog.Logger, operation string, sqlText string, argCount int, duration time.Duration, err error, extra ...slog.Attr) {
	attrs := []slog.Attr{
		slog.String("sql_operation", operation),
		slog.Float64("duration_ms", float64(duration)/float64(time.Millisecond)),
		slog.Int("arg_count", argCount),
		slog.String("sql", strings.TrimSpace(sqlText)),
	}
	attrs = append(attrs, extra...)
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, slog.LevelDebug, "sql executed", attrs...)
}
