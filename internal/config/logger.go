package config

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/trace"
)

const (
	logSourceWidth = 18
	logMsgWidth    = 28
	logTimeLayout  = "2006-01-02T15:04:05.000Z07:00"
)

func NewLogger(cfg *Config) *slog.Logger {
	return slog.New(newTextLogHandler(os.Stderr, LogLevel(cfg.Log.Level)))
}

func ConfigureLogger(cfg *Config) {
	slog.SetDefault(NewLogger(cfg))
}

type textLogHandler struct {
	out    io.Writer
	level  slog.Leveler
	mu     *sync.Mutex
	attrs  []slog.Attr
	groups []string
}

func newTextLogHandler(out io.Writer, level slog.Level) slog.Handler {
	return &textLogHandler{
		out:   out,
		level: level,
		mu:    &sync.Mutex{},
	}
}

func (h *textLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *textLogHandler) Handle(ctx context.Context, record slog.Record) error {
	var line bytes.Buffer
	spanContext := trace.SpanContextFromContext(ctx)
	writeFixed(&line, "time", record.Time.Format(logTimeLayout), 29)
	writeFixed(&line, "level", record.Level.String(), 5)
	writeFixed(&line, "source", sourceLocation(record.PC), logSourceWidth)
	writeFixed(&line, "msg", quoteIfNeeded(record.Message), logMsgWidth)
	writeAttr(&line, slog.String("trace_id", traceIDString(spanContext)), nil)
	writeAttr(&line, slog.String("span_id", spanIDString(spanContext)), nil)
	for _, attr := range h.attrs {
		writeAttr(&line, attr, nil)
	}
	record.Attrs(func(attr slog.Attr) bool {
		writeAttr(&line, attr, h.groups)
		return true
	})
	line.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.out.Write(line.Bytes())
	return err
}

func (h *textLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	for _, attr := range attrs {
		next.attrs = append(next.attrs, prefixAttr(attr, h.groups))
	}
	return next
}

func (h *textLogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := h.clone()
	next.groups = append(next.groups, name)
	return next
}

func (h *textLogHandler) clone() *textLogHandler {
	next := *h
	next.attrs = append([]slog.Attr(nil), h.attrs...)
	next.groups = append([]string(nil), h.groups...)
	return &next
}

func sourceLocation(pc uintptr) string {
	if pc == 0 {
		return "-"
	}
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.File == "" {
		return "-"
	}
	return frame.File + ":" + strconv.Itoa(frame.Line)
}

func traceIDString(spanContext trace.SpanContext) string {
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.TraceID().String()
}

func spanIDString(spanContext trace.SpanContext) string {
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.SpanID().String()
}

func writeFixed(line *bytes.Buffer, key string, value string, width int) {
	value = cleanLogValue(value)
	if len(value) > width {
		fmt.Fprintf(line, "%s=%s ", key, value)
		return
	}
	fmt.Fprintf(line, "%s=%-*s ", key, width, value)
}

func writeAttr(line *bytes.Buffer, attr slog.Attr, groups []string) {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		nextGroups := groups
		if attr.Key != "" {
			nextGroups = appendGroups(groups, attr.Key)
		}
		for _, child := range attr.Value.Group() {
			writeAttr(line, child, nextGroups)
		}
		return
	}
	key := groupedKey(groups, attr.Key)
	if key == "" {
		return
	}
	line.WriteString(key)
	line.WriteByte('=')
	line.WriteString(formatLogValue(attr.Value))
	line.WriteByte(' ')
}

func prefixAttr(attr slog.Attr, groups []string) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		children := attr.Value.Group()
		prefixed := make([]slog.Attr, 0, len(children))
		nextGroups := groups
		if attr.Key != "" {
			nextGroups = appendGroups(groups, attr.Key)
		}
		for _, child := range children {
			prefixed = append(prefixed, prefixAttr(child, nextGroups))
		}
		return slog.Attr{Value: slog.GroupValue(prefixed...)}
	}
	attr.Key = groupedKey(groups, attr.Key)
	return attr
}

func appendGroups(groups []string, group string) []string {
	next := make([]string, 0, len(groups)+1)
	next = append(next, groups...)
	next = append(next, group)
	return next
}

func groupedKey(groups []string, key string) string {
	if key == "" {
		return ""
	}
	if len(groups) == 0 {
		return key
	}
	return strings.Join(appendGroups(groups, key), ".")
}

func formatLogValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		return quoteIfNeeded(cleanLogValue(value.String()))
	case slog.KindBool:
		return strconv.FormatBool(value.Bool())
	case slog.KindInt64:
		return strconv.FormatInt(value.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(value.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(value.Float64(), 'f', -1, 64)
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().Format(logTimeLayout)
	default:
		return quoteIfNeeded(cleanLogValue(value.String()))
	}
}

func cleanLogValue(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		default:
			return r
		}
	}, value)
}

func quoteIfNeeded(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " =\"") {
		return strconv.Quote(value)
	}
	return value
}
