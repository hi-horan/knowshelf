// Package segment 封装用于 SQLite FTS 索引的 gse 分词。
package segment

import (
	"io"
	"log"
	"strings"
	"sync"
	"unicode"

	"github.com/go-ego/gse"
)

var logOutputMu sync.Mutex

// Segmenter 将自然语言文本转换成适合 FTS 的 token 字符串。
type Segmenter struct {
	seg gse.Segmenter
}

// New 创建基于 gse 的 Segmenter。
func New(dictionaries []string) (*Segmenter, error) {
	logOutputMu.Lock()
	defer logOutputMu.Unlock()
	oldOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldOutput)
	seg, err := gse.New(dictionaries...)
	if err != nil {
		return nil, err
	}
	return &Segmenter{seg: seg}, nil
}

// IndexText 返回用空格分隔、适配 FTS5 unicode61 的 token 流。
func (s *Segmenter) IndexText(text string) string {
	return strings.Join(s.QueryTerms(text), " ")
}

// QueryTerms 使用 gse 搜索模式返回归一化词项。
func (s *Segmenter) QueryTerms(text string) []string {
	raw := s.seg.CutSearch(text, true)
	return normalizeTerms(raw)
}

// BuildFTS5Query 使用前缀匹配构造 AND 查询。
func BuildFTS5Query(terms []string) string {
	terms = normalizeTerms(terms)
	if len(terms) == 0 {
		return ""
	}
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		parts = append(parts, `"`+strings.ReplaceAll(term, `"`, `""`)+`"*`)
	}
	return strings.Join(parts, " AND ")
}

func normalizeTerms(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = normalizeTerm(term)
		if term == "" {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

func normalizeTerm(term string) string {
	term = strings.TrimSpace(strings.ToLower(term))
	if term == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range term {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			if b.Len() > 0 {
				b.WriteRune(' ')
			}
		}
	}
	fields := strings.Fields(b.String())
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}
