package store

import (
	"fmt"
	"os"
)

func ReadMarkdownFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read markdown: %w", err)
	}
	return string(content), nil
}
