package crewai

import (
	"os"
	"strings"
	"sync"
)

// Task descreve uma unidade de trabalho a ser executada por um agente.
type Task struct {
	// Name é um identificador curto e opcional, útil em logs e memória.
	Name string
	// Description é a instrução detalhada do que deve ser feito. Suporta
	// interpolação de variáveis no formato {chave} via inputs do Kickoff.
	Description string
	// ExpectedOutput descreve o formato/qualidade esperados da resposta.
	ExpectedOutput string

	// Agent é o responsável pela tarefa. Se nil no processo sequencial, a
	// crew usa o próximo agente disponível.
	Agent *Agent

	// Tools, quando presente, substitui as ferramentas do agente para esta
	// tarefa específica.
	Tools []Tool

	// Context lista tarefas cujas saídas devem ser fornecidas como contexto
	// a esta tarefa.
	Context []*Task

	// OutputFile, se definido, faz a saída da tarefa ser gravada nesse arquivo.
	OutputFile string

	mu     sync.RWMutex
	output string
	done   bool
}

// NewTask cria uma tarefa com descrição, saída esperada e agente responsável.
func NewTask(description, expectedOutput string, agent *Agent) *Task {
	return &Task{
		Description:    description,
		ExpectedOutput: expectedOutput,
		Agent:          agent,
	}
}

// WithContext define as tarefas de contexto (dependências) desta tarefa.
func (t *Task) WithContext(tasks ...*Task) *Task {
	t.Context = append(t.Context, tasks...)
	return t
}

// Output devolve a saída já produzida pela tarefa (vazia se ainda não executada).
func (t *Task) Output() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.output
}

// setOutput registra a saída da tarefa e, se configurado, grava em arquivo.
func (t *Task) setOutput(out string) error {
	t.mu.Lock()
	t.output = out
	t.done = true
	file := t.OutputFile
	t.mu.Unlock()

	if file != "" {
		return os.WriteFile(file, []byte(out), 0o600)
	}
	return nil
}

// contextText monta o texto de contexto a partir das tarefas dependentes.
func (t *Task) contextText() string {
	var b strings.Builder
	for _, dep := range t.Context {
		out := dep.Output()
		if out == "" {
			continue
		}
		if dep.Name != "" {
			b.WriteString(dep.Name)
			b.WriteString(":\n")
		}
		b.WriteString(out)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

// interpolate substitui ocorrências de {chave} na descrição e na saída
// esperada usando o mapa de inputs.
func (t *Task) interpolate(inputs map[string]string) {
	if len(inputs) == 0 {
		return
	}
	t.Description = interpolate(t.Description, inputs)
	t.ExpectedOutput = interpolate(t.ExpectedOutput, inputs)
}

func interpolate(s string, inputs map[string]string) string {
	for k, v := range inputs {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}
