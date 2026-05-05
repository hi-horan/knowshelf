package segment

import "testing"

func TestSegmenterBuildFTS5Query(t *testing.T) {
	seg, err := New([]string{"zh"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	terms := seg.QueryTerms("孙悟空大闹天宫")
	if len(terms) == 0 {
		t.Fatal("expected terms")
	}
	query := BuildFTS5Query(terms)
	if query == "" {
		t.Fatal("expected fts query")
	}
}
