package crewai

import (
	"context"
	"fmt"
	"strings"
)

// executeTask roda o laço de raciocínio (ReAct) de um agente para uma tarefa.
func executeTask(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, error) {
	if a.LLM == nil {
		return "", ErrNoLLM
	}

	tools := effectiveTools(a, t)
	maxIter := a.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxIterations
	}

	system := buildSystemPrompt(a)
	if len(tools) > 0 {
		system += buildToolInstructions(tools)
	} else {
		system += noToolFinalInstruction
	}

	messages := []Message{
		SystemMessage(system),
		UserMessage(buildTaskPrompt(t, contextText)),
	}

	// Sem ferramentas: uma única chamada é suficiente.
	if len(tools) == 0 {
		out, err := a.LLM.Call(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("agente %q: %w", a.Role, err)
		}
		return strings.TrimSpace(stripFinalAnswer(out)), nil
	}

	for i := 0; i < maxIter; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		out, err := a.LLM.Call(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("agente %q: %w", a.Role, err)
		}
		out = strings.TrimSpace(out)
		log.Debugf("[%s] pensamento:\n%s", a.Role, out)

		if answer, ok := parseFinalAnswer(out); ok {
			return strings.TrimSpace(answer), nil
		}

		action, input, ok := parseAction(out)
		if !ok {
			// O modelo não seguiu o protocolo: tratamos a saída como
			// resposta final para não travar a execução.
			return strings.TrimSpace(out), nil
		}

		messages = append(messages, AssistantMessage(out))

		tool, found := findTool(tools, action)
		var observation string
		if !found {
			observation = fmt.Sprintf("Erro: ferramenta %q não existe. Ferramentas disponíveis: %s.",
				action, strings.Join(toolNames(tools), ", "))
		} else {
			log.Infof("[%s] usando ferramenta %q com entrada: %s", a.Role, action, input)
			result, err := tool.Call(ctx, input)
			if err != nil {
				observation = fmt.Sprintf("Erro ao executar a ferramenta %q: %v", action, err)
			} else {
				observation = result
			}
		}

		messages = append(messages, UserMessage("Observation: "+observation))
	}

	return "", fmt.Errorf("agente %q: %w (%d iterações)", a.Role, ErrMaxIterations, maxIter)
}

// effectiveTools combina as ferramentas do agente com as específicas da tarefa.
func effectiveTools(a *Agent, t *Task) []Tool {
	if t != nil && len(t.Tools) > 0 {
		// Ferramentas da tarefa têm prioridade e substituem as do agente.
		return t.Tools
	}
	return a.Tools
}

func toolNames(tools []Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name()
	}
	return names
}

// parseFinalAnswer extrai o texto após "Final Answer:".
func parseFinalAnswer(s string) (string, bool) {
	idx := indexMarker(s, "final answer:")
	if idx < 0 {
		return "", false
	}
	return s[idx:], true
}

// stripFinalAnswer remove um eventual prefixo "Final Answer:" de uma resposta
// direta (usado no caminho sem ferramentas).
func stripFinalAnswer(s string) string {
	if ans, ok := parseFinalAnswer(s); ok {
		return ans
	}
	return s
}

// parseAction extrai o nome da ferramenta ("Action:") e sua entrada
// ("Action Input:") de uma resposta do modelo.
func parseAction(s string) (action, input string, ok bool) {
	actIdx := indexMarker(s, "action:")
	if actIdx < 0 {
		return "", "", false
	}
	rest := s[actIdx:]

	inputIdx := indexMarker(rest, "action input:")
	if inputIdx < 0 {
		// Só há Action, sem Action Input.
		action = strings.TrimSpace(firstLine(rest))
		return action, "", action != ""
	}

	// A ação é o que estiver entre "Action:" e "Action Input:".
	actionPart := rest[:markerStart(rest, "action input:")]
	action = strings.TrimSpace(firstLine(actionPart))

	inputPart := rest[inputIdx:]
	// A entrada vai até a próxima "Observation:" ou o fim do texto.
	if obs := markerStart(inputPart, "observation:"); obs >= 0 {
		inputPart = inputPart[:obs]
	}
	input = strings.TrimSpace(inputPart)
	return action, input, action != ""
}

// indexMarker devolve o índice do início do CONTEÚDO logo após o marcador
// (case-insensitive), ou -1 se não encontrado.
func indexMarker(s, marker string) int {
	i := strings.Index(strings.ToLower(s), marker)
	if i < 0 {
		return -1
	}
	return i + len(marker)
}

// markerStart devolve o índice do INÍCIO do marcador (case-insensitive).
func markerStart(s, marker string) int {
	return strings.Index(strings.ToLower(s), marker)
}

// firstLine devolve a primeira linha não vazia de um trecho.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}
