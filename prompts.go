package crewai

import (
	"fmt"
	"strings"
)

// buildSystemPrompt monta a mensagem de sistema que define a persona do agente.
func buildSystemPrompt(a *Agent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Você é %s.\n", a.Role)
	if a.Backstory != "" {
		fmt.Fprintf(&b, "%s\n", a.Backstory)
	}
	if a.Goal != "" {
		fmt.Fprintf(&b, "\nSeu objetivo pessoal é: %s\n", a.Goal)
	}
	return strings.TrimSpace(b.String())
}

// buildToolInstructions descreve as ferramentas disponíveis e o protocolo ReAct
// que o modelo deve seguir para usá-las.
func buildToolInstructions(tools []Tool) string {
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	names := make([]string, 0, len(tools))
	b.WriteString("\nVocê tem acesso às seguintes ferramentas:\n\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "- %s: %s\n", t.Name(), t.Description())
		names = append(names, t.Name())
	}
	b.WriteString("\nPara usar uma ferramenta, responda EXATAMENTE neste formato:\n\n")
	b.WriteString("Thought: seu raciocínio sobre o que fazer\n")
	fmt.Fprintf(&b, "Action: o nome da ferramenta, exatamente um de [%s]\n", strings.Join(names, ", "))
	b.WriteString("Action Input: a entrada para a ferramenta\n\n")
	b.WriteString("O sistema responderá com:\n\n")
	b.WriteString("Observation: o resultado da ferramenta\n\n")
	b.WriteString("Repita esse ciclo (Thought/Action/Action Input) quantas vezes precisar. ")
	b.WriteString("Quando tiver a resposta final, responda EXATAMENTE neste formato:\n\n")
	b.WriteString("Thought: agora eu sei a resposta final\n")
	b.WriteString("Final Answer: a resposta final e completa para a tarefa\n")
	return b.String()
}

// buildTaskPrompt monta a mensagem de usuário com a tarefa a ser executada.
func buildTaskPrompt(t *Task, context string) string {
	var b strings.Builder
	b.WriteString("Tarefa atual:\n")
	b.WriteString(t.Description)
	b.WriteString("\n")
	if t.ExpectedOutput != "" {
		b.WriteString("\nFormato/objetivo esperado da resposta:\n")
		b.WriteString(t.ExpectedOutput)
		b.WriteString("\n")
	}
	if strings.TrimSpace(context) != "" {
		b.WriteString("\nContexto de tarefas anteriores:\n")
		b.WriteString(context)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// noToolFinalInstruction é adicionada quando o agente não tem ferramentas,
// para reforçar que ele deve responder diretamente.
const noToolFinalInstruction = "\nResponda diretamente com a melhor resposta possível para a tarefa, sem preâmbulos."
