package crewai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rodolphosa/crewai-go"
	"github.com/rodolphosa/crewai-go/llm/mock"
)

func TestCrewSequential(t *testing.T) {
	llm := mock.New("resultado da pesquisa", "resumo final")
	pesquisador := crewai.NewAgent("Pesquisador", "pesquisar", "", llm)
	escritor := crewai.NewAgent("Escritor", "escrever", "", llm)

	t1 := crewai.NewTask("Pesquise sobre Go", "fatos", pesquisador)
	t1.Name = "pesquisa"
	t2 := crewai.NewTask("Escreva um resumo", "texto", escritor).WithContext(t1)

	crew := crewai.NewCrew([]*crewai.Agent{pesquisador, escritor}, []*crewai.Task{t1, t2})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff erro: %v", err)
	}

	if out.Final != "resumo final" {
		t.Errorf("Final = %q", out.Final)
	}
	if len(out.TasksOutput) != 2 {
		t.Fatalf("esperava 2 saídas, obteve %d", len(out.TasksOutput))
	}
	if out.TasksOutput[0].Agent != "Pesquisador" {
		t.Errorf("agente da tarefa 1 = %q", out.TasksOutput[0].Agent)
	}
}

func TestCrewContextPropagation(t *testing.T) {
	// A segunda chamada do LLM deve receber, no prompt, a saída da 1ª tarefa.
	var secondPrompt string
	calls := 0
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		calls++
		if calls == 1 {
			return "SAIDA_UM", nil
		}
		for _, m := range msgs {
			secondPrompt += m.Content + "\n"
		}
		return "SAIDA_DOIS", nil
	}}

	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("tarefa 1", "", a)
	t2 := crewai.NewTask("tarefa 2", "", a).WithContext(t1)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("erro: %v", err)
	}
	if !strings.Contains(secondPrompt, "SAIDA_UM") {
		t.Errorf("contexto não propagado; prompt da 2ª tarefa: %q", secondPrompt)
	}
}

func TestCrewInputsInterpolation(t *testing.T) {
	var prompt string
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			prompt += m.Content
		}
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("Analise {empresa}", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	_, err := crew.Kickoff(context.Background(), map[string]string{"empresa": "Acme"})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if !strings.Contains(prompt, "Acme") {
		t.Errorf("interpolação falhou; prompt = %q", prompt)
	}
}

func TestCrewNoTasks(t *testing.T) {
	crew := crewai.NewCrew(nil, nil)
	if _, err := crew.Kickoff(context.Background(), nil); err != crewai.ErrNoTasks {
		t.Errorf("erro = %v, quer %v", err, crewai.ErrNoTasks)
	}
}

func TestCrewNoAgentForTask(t *testing.T) {
	task := crewai.NewTask("t", "", nil)
	crew := crewai.NewCrew(nil, []*crewai.Task{task})
	if _, err := crew.Kickoff(context.Background(), nil); err != crewai.ErrNoAgent {
		t.Errorf("erro = %v, quer %v", err, crewai.ErrNoAgent)
	}
}

func TestCrewMemory(t *testing.T) {
	llm := mock.New("primeiro", "segundo")
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a)
	t1.Name = "t1"
	t2 := crewai.NewTask("t2", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	crew.Memory = true
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("erro: %v", err)
	}

	mem := crew.MemorySnapshot()
	if mem == nil {
		t.Fatal("memória nil")
	}
	if len(mem.Records()) != 2 {
		t.Errorf("esperava 2 registros, obteve %d", len(mem.Records()))
	}
}

func TestCrewHierarchical(t *testing.T) {
	worker := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		return "trabalho feito", nil
	}}
	// O gerente sempre escolhe "Especialista".
	manager := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		return "Especialista", nil
	}}

	generalista := crewai.NewAgent("Generalista", "geral", "", worker)
	especialista := crewai.NewAgent("Especialista", "específico", "", worker)

	task := crewai.NewTask("faça o trabalho", "", nil) // sem agente => delegação

	crew := crewai.NewCrew([]*crewai.Agent{generalista, especialista}, []*crewai.Task{task})
	crew.Process = crewai.Hierarchical
	crew.ManagerLLM = manager

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if out.TasksOutput[0].Agent != "Especialista" {
		t.Errorf("delegação errada: %q", out.TasksOutput[0].Agent)
	}
}

func TestCrewHierarchicalNoManager(t *testing.T) {
	a := crewai.NewAgent("A", "", "", mock.New("x"))
	task := crewai.NewTask("t", "", nil)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Process = crewai.Hierarchical
	if _, err := crew.Kickoff(context.Background(), nil); err != crewai.ErrNoManager {
		t.Errorf("erro = %v, quer %v", err, crewai.ErrNoManager)
	}
}

func TestCrewInvalidProcess(t *testing.T) {
	a := crewai.NewAgent("A", "", "", mock.New("x"))
	task := crewai.NewTask("t", "", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Process = "paralelo"
	if _, err := crew.Kickoff(context.Background(), nil); err == nil {
		t.Error("esperava erro de processo inválido")
	}
}
