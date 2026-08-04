package crewai

import "errors"

var (
	// ErrNoLLM é retornado quando um agente é executado sem um LLM configurado.
	ErrNoLLM = errors.New("crewai: agente sem LLM configurado")
	// ErrNoAgent é retornado quando uma tarefa não tem agente atribuído e a
	// crew não consegue resolver um responsável.
	ErrNoAgent = errors.New("crewai: tarefa sem agente atribuído")
	// ErrNoTasks é retornado quando uma crew é iniciada sem tarefas.
	ErrNoTasks = errors.New("crewai: crew sem tarefas")
	// ErrNoManager é retornado quando o processo hierárquico é usado sem um
	// LLM (ManagerLLM) ou agente gerente (ManagerAgent).
	ErrNoManager = errors.New("crewai: processo hierárquico requer ManagerLLM ou ManagerAgent")
	// ErrMaxIterations é retornado quando o agente atinge o número máximo de
	// iterações sem produzir uma resposta final.
	ErrMaxIterations = errors.New("crewai: número máximo de iterações atingido")
)
