package crewai

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Crew reúne agentes e tarefas e as orquestra segundo um Process.
type Crew struct {
	// Agents são os membros da equipe.
	Agents []*Agent
	// Tasks são as tarefas a executar.
	Tasks []*Task
	// Process define a estratégia de orquestração. Padrão: Sequential.
	Process Process

	// Verbose ativa logs detalhados da execução.
	Verbose bool

	// Memory, se ativado, armazena as saídas das tarefas para consulta.
	Memory bool

	// ManagerLLM é o modelo usado pelo agente gerente no processo hierárquico.
	ManagerLLM LLM
	// ManagerAgent é um agente gerente explícito para o processo hierárquico.
	// Se definido, tem prioridade sobre ManagerLLM.
	ManagerAgent *Agent

	logger Logger
	mem    *Memory
}

// CrewOutput é o resultado da execução de uma crew.
type CrewOutput struct {
	// Final é a saída da última tarefa executada.
	Final string
	// TasksOutput contém a saída de cada tarefa, na ordem de execução.
	TasksOutput []TaskOutput
	// Duration é o tempo total de execução.
	Duration time.Duration
}

// TaskOutput é a saída de uma tarefa individual.
type TaskOutput struct {
	Task   string
	Agent  string
	Output string
}

// String devolve a saída final da crew.
func (o *CrewOutput) String() string { return o.Final }

// NewCrew cria uma crew sequencial com os agentes e tarefas informados.
func NewCrew(agents []*Agent, tasks []*Task) *Crew {
	return &Crew{
		Agents:  agents,
		Tasks:   tasks,
		Process: Sequential,
	}
}

// Kickoff executa todas as tarefas da crew e devolve o resultado consolidado.
//
// inputs é um mapa opcional de variáveis interpoladas em {chave} nas
// descrições e saídas esperadas das tarefas.
func (c *Crew) Kickoff(ctx context.Context, inputs map[string]string) (*CrewOutput, error) {
	if len(c.Tasks) == 0 {
		return nil, ErrNoTasks
	}
	if c.Process == "" {
		c.Process = Sequential
	}
	if !c.Process.valid() {
		return nil, fmt.Errorf("crewai: processo inválido %q", c.Process)
	}

	c.logger = newStdLogger(c.Verbose)
	if c.Memory {
		c.mem = NewMemory()
	}

	// Interpola inputs em todas as tarefas.
	for _, t := range c.Tasks {
		t.interpolate(inputs)
	}

	start := time.Now()
	var err error
	out := &CrewOutput{}

	switch c.Process {
	case Sequential:
		out, err = c.runSequential(ctx)
	case Hierarchical:
		out, err = c.runHierarchical(ctx)
	}
	if err != nil {
		return nil, err
	}
	out.Duration = time.Since(start)
	return out, nil
}

// runSequential executa as tarefas em ordem.
func (c *Crew) runSequential(ctx context.Context) (*CrewOutput, error) {
	out := &CrewOutput{}
	for i, task := range c.Tasks {
		agent := task.Agent
		if agent == nil {
			agent = c.agentForIndex(i)
		}
		if agent == nil {
			return nil, ErrNoAgent
		}

		result, err := c.execute(ctx, agent, task)
		if err != nil {
			return nil, fmt.Errorf("tarefa %d: %w", i+1, err)
		}
		out.TasksOutput = append(out.TasksOutput, TaskOutput{
			Task:   taskLabel(task, i),
			Agent:  agent.Role,
			Output: result,
		})
		out.Final = result
	}
	return out, nil
}

// runHierarchical usa um gerente para atribuir cada tarefa ao melhor agente.
func (c *Crew) runHierarchical(ctx context.Context) (*CrewOutput, error) {
	manager, err := c.resolveManager()
	if err != nil {
		return nil, err
	}
	c.logger.Infof("gerente: %s", manager.Role)

	out := &CrewOutput{}
	for i, task := range c.Tasks {
		agent := task.Agent
		if agent == nil {
			agent = c.delegate(ctx, manager, task)
		}
		if agent == nil {
			return nil, ErrNoAgent
		}
		c.logger.Infof("gerente delegou a tarefa %d para %q", i+1, agent.Role)

		result, err := c.execute(ctx, agent, task)
		if err != nil {
			return nil, fmt.Errorf("tarefa %d: %w", i+1, err)
		}
		out.TasksOutput = append(out.TasksOutput, TaskOutput{
			Task:   taskLabel(task, i),
			Agent:  agent.Role,
			Output: result,
		})
		out.Final = result
	}
	return out, nil
}

// execute roda uma tarefa, monta o contexto e persiste a saída/memória.
func (c *Crew) execute(ctx context.Context, agent *Agent, task *Task) (string, error) {
	contextText := task.contextText()
	if c.mem != nil && contextText == "" {
		// Sem contexto explícito, injeta a memória acumulada.
		if mem := strings.TrimSpace(c.mem.String()); mem != "" {
			contextText = mem
		}
	}

	result, err := executeTask(ctx, agent, task, contextText, c.logger)
	if err != nil {
		return "", err
	}
	if err := task.setOutput(result); err != nil {
		return "", fmt.Errorf("gravando saída da tarefa: %w", err)
	}
	if c.mem != nil {
		c.mem.Save(MemoryRecord{
			Agent:   agent.Role,
			Task:    task.Name,
			Content: result,
		})
	}
	return result, nil
}

// resolveManager devolve o agente gerente do processo hierárquico.
func (c *Crew) resolveManager() (*Agent, error) {
	if c.ManagerAgent != nil {
		return c.ManagerAgent, nil
	}
	if c.ManagerLLM != nil {
		return &Agent{
			Role:      "Gerente da Equipe",
			Goal:      "Coordenar a equipe para concluir as tarefas com excelência.",
			Backstory: "Você é um gerente experiente, hábil em delegar trabalho para o membro mais adequado da equipe.",
			LLM:       c.ManagerLLM,
		}, nil
	}
	return nil, ErrNoManager
}

// delegate pede ao gerente que escolha o melhor agente para a tarefa.
func (c *Crew) delegate(ctx context.Context, manager *Agent, task *Task) *Agent {
	if len(c.Agents) == 0 {
		return nil
	}
	if len(c.Agents) == 1 {
		return c.Agents[0]
	}

	var b strings.Builder
	b.WriteString("Você deve escolher qual membro da equipe é o mais adequado para a tarefa a seguir.\n\n")
	b.WriteString("Membros disponíveis:\n")
	for _, a := range c.Agents {
		fmt.Fprintf(&b, "- %s: %s\n", a.Role, a.Goal)
	}
	fmt.Fprintf(&b, "\nTarefa: %s\n", task.Description)
	b.WriteString("\nResponda APENAS com o papel exato do membro escolhido, sem nenhum outro texto.")

	resp, err := manager.LLM.Call(ctx, []Message{
		SystemMessage(buildSystemPrompt(manager)),
		UserMessage(b.String()),
	})
	if err != nil {
		c.logger.Infof("falha na delegação, usando primeiro agente: %v", err)
		return c.Agents[0]
	}

	resp = strings.TrimSpace(resp)
	// Match exato e, na falta, match por substring.
	for _, a := range c.Agents {
		if strings.EqualFold(strings.TrimSpace(resp), a.Role) {
			return a
		}
	}
	for _, a := range c.Agents {
		if strings.Contains(strings.ToLower(resp), strings.ToLower(a.Role)) {
			return a
		}
	}
	return c.Agents[0]
}

// agentForIndex escolhe um agente para a tarefa i quando ela não tem agente.
func (c *Crew) agentForIndex(i int) *Agent {
	if len(c.Agents) == 0 {
		return nil
	}
	if i < len(c.Agents) {
		return c.Agents[i]
	}
	return c.Agents[len(c.Agents)-1]
}

// MemorySnapshot devolve a memória acumulada (nil se a memória estiver desativada).
func (c *Crew) MemorySnapshot() *Memory { return c.mem }

func taskLabel(t *Task, i int) string {
	if t.Name != "" {
		return t.Name
	}
	return fmt.Sprintf("Tarefa %d", i+1)
}
