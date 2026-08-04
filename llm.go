package crewai

import "context"

// Role identifica o autor de uma mensagem em uma conversa com o modelo.
type Role string

const (
	// RoleSystem contém as instruções de sistema (persona/regras) do agente.
	RoleSystem Role = "system"
	// RoleUser representa a entrada do usuário (a tarefa a ser executada).
	RoleUser Role = "user"
	// RoleAssistant representa a resposta gerada pelo modelo.
	RoleAssistant Role = "assistant"
	// RoleTool representa o resultado da execução de uma ferramenta.
	RoleTool Role = "tool"
)

// Message é uma única mensagem trocada com o modelo de linguagem.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// LLM é a abstração mínima que qualquer provedor de modelo de linguagem
// precisa implementar para ser usado pelos agentes.
//
// A implementação deve ser segura para uso concorrente, pois o mesmo LLM
// pode ser compartilhado por vários agentes executando em paralelo.
type LLM interface {
	// Call envia a conversa ao modelo e retorna o texto gerado.
	Call(ctx context.Context, messages []Message) (string, error)
	// Model devolve o identificador do modelo (ex.: "gpt-4o-mini").
	Model() string
}

// SystemMessage é um atalho para criar uma mensagem de sistema.
func SystemMessage(content string) Message {
	return Message{Role: RoleSystem, Content: content}
}

// UserMessage é um atalho para criar uma mensagem de usuário.
func UserMessage(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

// AssistantMessage é um atalho para criar uma mensagem do assistente.
func AssistantMessage(content string) Message {
	return Message{Role: RoleAssistant, Content: content}
}
