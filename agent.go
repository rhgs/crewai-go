package crewai

import "context"

const defaultMaxIterations = 15

// Agent é um trabalhador autônomo com um papel, um objetivo e uma história.
// Ele usa um LLM para raciocinar e, opcionalmente, ferramentas para agir,
// executando as tarefas que a crew lhe atribui.
//
// O zero value não é utilizável; crie agentes com NewAgent ou preenchendo os
// campos obrigatórios (Role e LLM) diretamente.
type Agent struct {
	// Role é a função/papel do agente (ex.: "Pesquisador Sênior").
	Role string
	// Goal é o objetivo pessoal que guia as decisões do agente.
	Goal string
	// Backstory dá contexto e personalidade ao agente.
	Backstory string

	// LLM é o modelo de linguagem usado pelo agente. Obrigatório.
	LLM LLM

	// Tools são as ferramentas que o agente pode usar em qualquer tarefa.
	Tools []Tool

	// MaxIterations limita o número de ciclos de raciocínio/uso de ferramentas
	// por tarefa. Se <= 0, usa o padrão (15).
	MaxIterations int

	// AllowDelegation habilita este agente a ser considerado como gerente
	// no processo hierárquico e a delegar trabalho (informativo nesta versão).
	AllowDelegation bool
}

// NewAgent cria um agente com os campos essenciais preenchidos.
func NewAgent(role, goal, backstory string, llm LLM) *Agent {
	return &Agent{
		Role:      role,
		Goal:      goal,
		Backstory: backstory,
		LLM:       llm,
	}
}

// WithTools adiciona ferramentas ao agente e devolve o próprio agente para
// encadeamento fluente.
func (a *Agent) WithTools(tools ...Tool) *Agent {
	a.Tools = append(a.Tools, tools...)
	return a
}

// Execute roda uma tarefa isolada com este agente e devolve a saída. É útil
// para usar um agente fora de uma crew ou em testes.
func (a *Agent) Execute(ctx context.Context, t *Task) (string, error) {
	return executeTask(ctx, a, t, "", nopLogger{})
}
