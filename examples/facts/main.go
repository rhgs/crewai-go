// Facts & provenance example: a FactSource connector tool paired with a
// provenance guardrail.
//
// Run:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/facts
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

func main() {
	llm := openai.New("gpt-4o-mini")

	auditor := crewai.NewAgent(
		"Auditor",
		"Audit company status",
		"You verify company registration status using the CNPJ lookup tool.",
		llm,
	)

	tool := crewai.NewFactSourceTool(
		"cnpj_lookup",
		"Looks up a CNPJ registration status. Input: the CNPJ number.",
		func(_ context.Context, cnpj string) (string, error) {
			// In production, this would call the Receita Federal API.
			rawPayload := []byte(`{"cnpj":"` + cnpj + `","razao_social":"Empresa Exemplo Ltda","situacao":"ATIVA"}`)
			fact := crewai.NewFact(
				"Empresa Exemplo Ltda (CNPJ "+cnpj+") is ATIVA",
				"Receita Federal",
				"https://api.receita.gov.br/v1/cnpj/"+cnpj,
				rawPayload,
			)
			return fact.Claim, nil
		},
		func(_ context.Context, output string) []crewai.Fact {
			return []crewai.Fact{
				crewai.NewFact(output, "Receita Federal",
					"https://api.receita.gov.br/v1/cnpj/00000000000100",
					[]byte(output)),
			}
		},
	)
	auditor.WithTools(tool)

	task := crewai.NewTask(
		"Verify the registration status of CNPJ 00.000.000/0001-00.",
		"A statement about the company's registration status.",
		auditor,
	)

	crew := crewai.NewCrew([]*crewai.Agent{auditor}, []*crewai.Task{task})
	crew.Guardrails = []crewai.Guardrail{
		func(_ context.Context, out *crewai.CrewOutput) error {
			return crewai.AllFactsProvenanced(out.Facts)
		},
	}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatalf("blocked: %v", err)
	}

	fmt.Println("\n=== Result ===")
	fmt.Println(out.Final)
	fmt.Printf("\n=== Facts (%d) ===\n", len(out.Facts))
	for _, f := range out.Facts {
		fmt.Printf("- %s\n  source: %s\n  hash: %s\n", f.Claim, f.SourceURL, f.PayloadHash)
	}
}
