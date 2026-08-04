package crewai

import "testing"

func TestMemorySaveAndSearch(t *testing.T) {
	m := NewMemory()
	m.Save(MemoryRecord{Agent: "A", Task: "pesquisa", Content: "Go é rápido"})
	m.Save(MemoryRecord{Agent: "B", Task: "redação", Content: "Python é popular"})

	if len(m.Records()) != 2 {
		t.Fatalf("esperava 2 registros, obteve %d", len(m.Records()))
	}

	got := m.Search("go")
	if len(got) != 1 || got[0].Content != "Go é rápido" {
		t.Errorf("Search(go) = %+v", got)
	}

	if len(m.Search("")) != 2 {
		t.Error("busca vazia deveria devolver todos")
	}
	if len(m.Search("inexistente")) != 0 {
		t.Error("busca sem correspondência deveria devolver zero")
	}
}

func TestMemoryString(t *testing.T) {
	m := NewMemory()
	m.Save(MemoryRecord{Agent: "A", Content: "linha um"})
	s := m.String()
	if s == "" || s[0] != '-' {
		t.Errorf("String() = %q", s)
	}
}
