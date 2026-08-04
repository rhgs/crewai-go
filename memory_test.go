package crewai

import "testing"

func TestMemorySaveAndSearch(t *testing.T) {
	m := NewMemory()
	m.Save(MemoryRecord{Agent: "A", Task: "research", Content: "Go is fast"})
	m.Save(MemoryRecord{Agent: "B", Task: "writing", Content: "Python is popular"})

	if len(m.Records()) != 2 {
		t.Fatalf("expected 2 records, got %d", len(m.Records()))
	}

	got := m.Search("go")
	if len(got) != 1 || got[0].Content != "Go is fast" {
		t.Errorf("Search(go) = %+v", got)
	}

	if len(m.Search("")) != 2 {
		t.Error("an empty search should return all records")
	}
	if len(m.Search("nonexistent")) != 0 {
		t.Error("a search with no match should return zero")
	}
}

func TestMemoryString(t *testing.T) {
	m := NewMemory()
	m.Save(MemoryRecord{Agent: "A", Content: "line one"})
	s := m.String()
	if s == "" || s[0] != '-' {
		t.Errorf("String() = %q", s)
	}
}
