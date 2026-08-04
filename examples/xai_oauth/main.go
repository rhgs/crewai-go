// xAI (Grok) example: authentication via API key OR subscription OAuth
// (SuperGrok / X Premium), with no per-token key.
//
// API key mode:
//
//	export XAI_API_KEY=xai-...
//	go run ./examples/xai_oauth
//
// Subscription OAuth mode (one-time login via Device Flow; the token is saved
// to disk and renewed automatically):
//
//	export XAI_CLIENT_ID=<official_xai_client_id>
//	XAI_OAUTH=1 go run ./examples/xai_oauth
//
// Note: xAI does not yet officially publish the client_id/OAuth endpoints for
// third parties. Provide them via XAI_CLIENT_ID and, if necessary, adjust the
// endpoints in the code (WithDeviceCodeURL/WithTokenURL) per the official docs.
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

	agent := crewai.NewAgent(
		"Grok Analyst",
		"Answer with sharp, concise reasoning",
		"You are Grok, direct and insightful.",
		llm,
	)
	task := crewai.NewTask(
		"In one sentence, why is Go popular for microservices?",
		"One sentence.",
		agent,
	)

	crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out.Final)
}

func buildLLM() crewai.LLM {
	if os.Getenv("XAI_OAUTH") == "" {
		// Simple mode: API key (XAI_API_KEY).
		return xai.New("grok-4")
	}

	// Subscription OAuth mode.
	clientID := os.Getenv("XAI_CLIENT_ID")
	if clientID == "" {
		log.Fatal("set XAI_CLIENT_ID for OAuth mode")
	}
	df := xai.NewDeviceFlow(clientID)

	home, _ := os.UserHomeDir()
	tokenPath := filepath.Join(home, ".crewai-xai-token.json")

	// Try to reuse a saved token (auto-renews when expired).
	if ts, err := xai.LoadTokenSource(tokenPath, df); err == nil {
		return xai.NewWithOAuth("grok-4", ts)
	}

	// First login: run the Device Flow.
	tok, err := df.Authorize(context.Background())
	if err != nil {
		log.Fatalf("OAuth login failed: %v", err)
	}
	if err := xai.SaveToken(tokenPath, tok); err != nil {
		log.Printf("warning: could not save the token: %v", err)
	}
	ts := df.TokenSource(tok, func(t xai.Token) error { return xai.SaveToken(tokenPath, t) })
	return xai.NewWithOAuth("grok-4", ts)
}
