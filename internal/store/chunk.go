package store

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	einomarkdown "github.com/cloudwego/eino-ext/components/document/transformer/splitter/markdown"
	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/recursive"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultParentChunkRunes = 2000
	defaultChildChunkRunes  = 1000
	defaultOverlapRunes     = 0
)

type chunkedMarkdown struct {
	Parents  []ParentChunk
	Children []childChunk
}

type childChunk struct {
	Chunk       Chunk
	ParentIndex int
}

func chunkMarkdown(ctx context.Context, content, title string) (chunkedMarkdown, error) {
	headerSplitter, err := newMarkdownHeaderSplitter(ctx)
	if err != nil {
		return chunkedMarkdown{}, err
	}
	parentSplitter, err := newRecursiveSplitter(ctx, "parent", defaultParentChunkRunes, defaultOverlapRunes)
	if err != nil {
		return chunkedMarkdown{}, err
	}
	childSplitter, err := newRecursiveSplitter(ctx, "child", defaultChildChunkRunes, defaultOverlapRunes)
	if err != nil {
		return chunkedMarkdown{}, err
	}

	headerDocs, err := headerSplitter.Transform(ctx, []*schema.Document{{
		ID:      "source",
		Content: content,
	}})
	if err != nil {
		return chunkedMarkdown{}, fmt.Errorf("split markdown headers: %w", err)
	}
	if len(headerDocs) == 0 {
		headerDocs = []*schema.Document{{ID: "source", Content: content}}
	}

	var out chunkedMarkdown
	for _, headerDoc := range headerDocs {
		if headerDoc == nil || strings.TrimSpace(headerDoc.Content) == "" {
			continue
		}
		parentDocs, err := parentSplitter.Transform(ctx, []*schema.Document{headerDoc})
		if err != nil {
			return chunkedMarkdown{}, fmt.Errorf("split parent chunks: %w", err)
		}
		for _, parentDoc := range parentDocs {
			if parentDoc == nil {
				continue
			}
			parentText := strings.TrimSpace(parentDoc.Content)
			if parentText == "" || isHeadingOnlyChunk(parentText) {
				continue
			}

			parentIndex := len(out.Parents)
			parent := ParentChunk{
				Title:       title,
				HeadingPath: headingPathFromMetadata(parentDoc.MetaData, title),
				Text:        parentText,
			}
			children, err := childChunksForParent(ctx, childSplitter, parentDoc, title, parentIndex)
			if err != nil {
				return chunkedMarkdown{}, err
			}
			if len(children) == 0 {
				continue
			}
			out.Parents = append(out.Parents, parent)
			out.Children = append(out.Children, children...)
		}
	}
	return out, nil
}

func childChunksForParent(ctx context.Context, childSplitter document.Transformer, parentDoc *schema.Document, title string, parentIndex int) ([]childChunk, error) {
	childDocs, err := childSplitter.Transform(ctx, []*schema.Document{parentDoc})
	if err != nil {
		return nil, fmt.Errorf("split child chunks: %w", err)
	}
	chunks := make([]childChunk, 0, len(childDocs))
	for _, doc := range childDocs {
		if doc == nil {
			continue
		}
		text := strings.TrimSpace(doc.Content)
		if text == "" || isHeadingOnlyChunk(text) {
			continue
		}
		chunks = append(chunks, childChunk{
			ParentIndex: parentIndex,
			Chunk: Chunk{
				Title:       title,
				HeadingPath: headingPathFromMetadata(doc.MetaData, title),
				Text:        text,
			},
		})
	}
	return chunks, nil
}

func newMarkdownHeaderSplitter(ctx context.Context) (document.Transformer, error) {
	return einomarkdown.NewHeaderSplitter(ctx, &einomarkdown.HeaderConfig{
		Headers: map[string]string{
			"#":      "h1",
			"##":     "h2",
			"###":    "h3",
			"####":   "h4",
			"#####":  "h5",
			"######": "h6",
		},
		TrimHeaders: false,
		IDGenerator: func(ctx context.Context, originalID string, splitIndex int) string {
			return fmt.Sprintf("%s_s_%04d", originalID, splitIndex+1)
		},
	})
}

func newRecursiveSplitter(ctx context.Context, idPrefix string, chunkRunes, overlapRunes int) (document.Transformer, error) {
	return recursive.NewSplitter(ctx, &recursive.Config{
		ChunkSize:   chunkRunes,
		OverlapSize: overlapRunes,
		Separators:  []string{"\n\n", "\n", "。", "；", "，", ".", "?", "!", ""},
		LenFunc:     utf8.RuneCountInString,
		KeepType:    recursive.KeepTypeEnd,
		IDGenerator: func(ctx context.Context, originalID string, splitIndex int) string {
			return fmt.Sprintf("%s_%s_%04d", originalID, idPrefix, splitIndex+1)
		},
	})
}

func headingPathFromMetadata(metadata map[string]any, fallbackTitle string) string {
	parts := make([]string, 0, 6)
	for i := 1; i <= 6; i++ {
		value, ok := metadata[fmt.Sprintf("h%d", i)].(string)
		if ok && value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return fallbackTitle
	}
	return strings.Join(parts, " / ")
}

func isHeadingOnlyChunk(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	hasHeading := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !headingRE.MatchString(line) {
			return false
		}
		hasHeading = true
	}
	return hasHeading
}
