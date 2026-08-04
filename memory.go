package crewai

import (
	"strings"
	"sync"
)

// Memory guarda registros produzidos ao longo da execução de uma crew para que
// tarefas posteriores possam recuperar contexto de tarefas anteriores.
//
// Esta é uma implementação simples em memória e segura para concorrência.
// Para cenários avançados (busca semântica/embeddings) basta fornecer sua
// própria implementação satisfazendo a mesma interface mínima usada pela Crew.
type Memory struct {
	mu      sync.RWMutex
	records []MemoryRecord
}

// MemoryRecord é uma anotação armazenada na memória.
type MemoryRecord struct {
	// Agent é o papel (role) do agente que gerou o registro.
	Agent string
	// Task é o nome/descrição curta da tarefa relacionada.
	Task string
	// Content é o conteúdo memorizado (normalmente a saída da tarefa).
	Content string
}

// NewMemory cria uma memória vazia.
func NewMemory() *Memory {
	return &Memory{}
}

// Save adiciona um registro à memória.
func (m *Memory) Save(rec MemoryRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rec)
}

// Records devolve uma cópia de todos os registros armazenados.
func (m *Memory) Records() []MemoryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MemoryRecord, len(m.records))
	copy(out, m.records)
	return out
}

// Search faz uma busca textual simples (substring, case-insensitive) e
// devolve os registros que contêm a consulta. Uma consulta vazia devolve
// todos os registros.
func (m *Memory) Search(query string) []MemoryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if strings.TrimSpace(query) == "" {
		out := make([]MemoryRecord, len(m.records))
		copy(out, m.records)
		return out
	}
	q := strings.ToLower(query)
	var out []MemoryRecord
	for _, r := range m.records {
		if strings.Contains(strings.ToLower(r.Content), q) ||
			strings.Contains(strings.ToLower(r.Task), q) {
			out = append(out, r)
		}
	}
	return out
}

// String devolve uma representação legível de toda a memória, útil para
// injetar como contexto em prompts.
func (m *Memory) String() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var b strings.Builder
	for _, r := range m.records {
		b.WriteString("- [")
		b.WriteString(r.Agent)
		b.WriteString("] ")
		b.WriteString(r.Content)
		b.WriteString("\n")
	}
	return b.String()
}
