// Package crewai é um port idiomático em Go do framework CrewAI para
// orquestração de agentes de IA autônomos e colaborativos.
//
// O framework gira em torno de quatro tipos principais:
//
//   - Agent: um trabalhador com papel, objetivo, história, um LLM e ferramentas.
//   - Task:  uma unidade de trabalho com descrição, saída esperada e responsável.
//   - Crew:  agrupa agentes e tarefas e as orquestra segundo um Process.
//   - Tool:  uma capacidade que o agente pode invocar durante o raciocínio.
//
// Um LLM é qualquer tipo que implemente a interface LLM; implementações prontas
// estão nos subpacotes llm/openai, llm/anthropic e llm/mock.
//
// Exemplo mínimo:
//
//	llm := openai.New("gpt-4o-mini")
//	agente := crewai.NewAgent("Poeta", "Escrever poemas", "...", llm)
//	tarefa := crewai.NewTask("Escreva um haikai sobre Go.", "3 versos", agente)
//	crew := crewai.NewCrew([]*crewai.Agent{agente}, []*crewai.Task{tarefa})
//	out, err := crew.Kickoff(context.Background(), nil)
//
// A orquestração pode ser sequencial (Sequential) ou hierárquica
// (Hierarchical), esta última com um gerente que delega tarefas dinamicamente.
package crewai
