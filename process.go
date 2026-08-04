package crewai

// Process define a estratégia de orquestração das tarefas em uma crew.
type Process string

const (
	// Sequential executa as tarefas na ordem em que foram definidas, passando
	// a saída de cada uma como contexto para as seguintes.
	Sequential Process = "sequential"

	// Hierarchical usa um agente gerente (ou um ManagerLLM) para coordenar a
	// execução, decidindo qual agente executa cada tarefa.
	Hierarchical Process = "hierarchical"
)

// valid indica se o processo é reconhecido.
func (p Process) valid() bool {
	switch p {
	case Sequential, Hierarchical:
		return true
	default:
		return false
	}
}
