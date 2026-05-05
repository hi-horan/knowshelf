package models

import (
	"context"
	"testing"

	"knowshelf/internal/config"
)

func TestEmbed(t *testing.T) {
	cfg, err := config.Load("../../config.yaml")
	if err != nil {
		t.Fatalf("failed to get config. err=%v", err)
	}
	ctx := context.Background()
	t.Logf("cfg=%#v", cfg.Models.Embedding)

	embed, err := NewEmbedder(ctx, &cfg.Models.Embedding)
	if err != nil {
		t.Fatalf("failed to get embedder. err=%v", err)
	}

	ans, err := embed.EmbedStrings(ctx, []string{"你好"})
	if err != nil {
		t.Fatalf("failed to embed. err=%v", err)
	}
	t.Logf("embed over. vector=%v", ans)
}
