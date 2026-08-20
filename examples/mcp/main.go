// Example: attach MCP tools to an agent with least-privilege guards.
//
// Without a live MCP server this program only demonstrates the wiring
// (FilterTools + WithDescriptionLimit + NewToolAdapter). Point MCP_ENDPOINT
// at a Streamable HTTP MCP server to list tools and run a tiny Kickoff.
//
//	# wiring only (no network):
//	go run ./examples/mcp
//
//	# live (optional):
//	export MCP_ENDPOINT=https://mcp.example.com/sse
//	export MCP_TOKEN=...           # optional Bearer
//	export OPENAI_API_KEY=sk-...   # only if you want a real Kickoff
//	go run ./examples/mcp
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
	"github.com/rhgs/crewai-go/mcp"
)

func main() {
	endpoint := strings.TrimSpace(os.Getenv("MCP_ENDPOINT"))
	if endpoint == "" {
		printWiring()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	opts := []mcp.Option{
		mcp.WithHTTPTimeout(30 * time.Second),
	}
	if tok := strings.TrimSpace(os.Getenv("MCP_TOKEN")); tok != "" {
		opts = append(opts, mcp.WithHeader("Authorization", "Bearer "+tok))
	}

	client := mcp.New(endpoint, opts...)
	defer client.Close(context.Background())

	if err := client.Initialize(ctx, "crewai-go-example", "0.5.0"); err != nil {
		fmt.Fprintln(os.Stderr, "initialize:", err)
		os.Exit(1)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list tools:", err)
		os.Exit(1)
	}
	fmt.Printf("listed %d tools from %s\n", len(tools), endpoint)

	// Optional allowlist via MCP_ALLOW (comma-separated names). Empty = keep all.
	if allow := strings.TrimSpace(os.Getenv("MCP_ALLOW")); allow != "" {
		set := map[string]struct{}{}
		for _, n := range strings.Split(allow, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				set[n] = struct{}{}
			}
		}
		tools = mcp.FilterTools(tools, set)
		fmt.Printf("after FilterTools: %d tools\n", len(tools))
	}

	var crewTools []crewai.Tool
	for _, t := range tools {
		crewTools = append(crewTools, mcp.NewToolAdapter(client, t, mcp.WithDescriptionLimit(500)))
	}

	if os.Getenv("OPENAI_API_KEY") == "" || len(crewTools) == 0 {
		for _, t := range crewTools {
			fmt.Printf("- %s: %s\n", t.Name(), truncate(t.Description(), 80))
		}
		fmt.Println("OK — set OPENAI_API_KEY (and tools) to run a Kickoff")
		return
	}

	llm := openai.New("gpt-4o-mini")
	agent := crewai.NewAgent(
		"MCP Operator",
		"Use attached MCP tools carefully and answer briefly",
		"You only call tools when needed and summarize results.",
		llm,
	).WithTools(crewTools...)

	task := crewai.NewTask(
		"List the tools you have and answer in one short paragraph what they are for.",
		"A short paragraph.",
		agent,
	)
	crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})
	crew.Verbose = true

	out, err := crew.Kickoff(ctx, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kickoff:", err)
		os.Exit(1)
	}
	fmt.Println(out.Final)
}

func printWiring() {
	fmt.Println("MCP example wiring (no MCP_ENDPOINT set)")
	fmt.Println("  client := mcp.New(endpoint, mcp.WithHTTPTimeout(30*time.Second))")
	fmt.Println("  tools  = mcp.FilterTools(tools, allowset)           // optional")
	fmt.Println("  adapter:= mcp.NewToolAdapter(c, t, mcp.WithDescriptionLimit(500))")
	fmt.Println("See docs/en/mcp.md for threat model and JSON config.")
	fmt.Println("OK — set MCP_ENDPOINT to list tools from a live server")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
