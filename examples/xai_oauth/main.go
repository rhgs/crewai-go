// Exemplo xAI (Grok): autenticação por chave de API OU por OAuth de assinatura
// (SuperGrok / X Premium), sem chave cobrada por token.
//
// Modo chave de API:
//
//	export XAI_API_KEY=xai-...
//	go run ./examples/xai_oauth
//
// Modo OAuth de assinatura (login único via Device Flow; o token é salvo em
// disco e renovado automaticamente):
//
//	export XAI_CLIENT_ID=<client_id_oficial_da_xai>
//	XAI_OAUTH=1 go run ./examples/xai_oauth
//
// Observação: a xAI ainda não publica oficialmente o client_id/endpoints de
// OAuth para terceiros. Informe-os via XAI_CLIENT_ID e, se necessário, ajuste
// os endpoints no código (WithDeviceCodeURL/WithTokenURL) conforme a doc oficial.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/xai"
)

func main() {
	llm := buildLLM()

	agente := crewai.NewAgent(
		"Analista Grok",
		"Responder com raciocínio afiado e conciso",
		"Você é o Grok, direto e perspicaz.",
		llm,
	)
	tarefa := crewai.NewTask(
		"Em uma frase, por que Go é popular para microsserviços?",
		"Uma frase.",
		agente,
	)

	crew := crewai.NewCrew([]*crewai.Agent{agente}, []*crewai.Task{tarefa})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out.Final)
}

func buildLLM() crewai.LLM {
	if os.Getenv("XAI_OAUTH") == "" {
		// Modo simples: chave de API (XAI_API_KEY).
		return xai.New("grok-4")
	}

	// Modo OAuth de assinatura.
	clientID := os.Getenv("XAI_CLIENT_ID")
	if clientID == "" {
		log.Fatal("defina XAI_CLIENT_ID para o modo OAuth")
	}
	df := xai.NewDeviceFlow(clientID)

	home, _ := os.UserHomeDir()
	tokenPath := filepath.Join(home, ".crewai-xai-token.json")

	// Tenta reutilizar um token já salvo (renova sozinho quando expira).
	if ts, err := xai.LoadTokenSource(tokenPath, df); err == nil {
		return xai.NewWithOAuth("grok-4", ts)
	}

	// Primeiro login: executa o Device Flow.
	tok, err := df.Authorize(context.Background())
	if err != nil {
		log.Fatalf("login OAuth falhou: %v", err)
	}
	if err := xai.SaveToken(tokenPath, tok); err != nil {
		log.Printf("aviso: não foi possível salvar o token: %v", err)
	}
	ts := df.TokenSource(tok, func(t xai.Token) error { return xai.SaveToken(tokenPath, t) })
	return xai.NewWithOAuth("grok-4", ts)
}
