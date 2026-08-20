package crewai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultMaxDelegationDepth is the maximum nested delegate_to_coworker
// depth allowed in a single Kickoff (or standalone Execute) call stack.
// The first delegation is depth 1.
const DefaultMaxDelegationDepth = 2

// delegationToolName is the fixed tool name exposed to the model.
const delegationToolName = "delegate_to_coworker"

// DelegationRoster supplies the agents that may be targeted by
// delegate_to_coworker. Crew implements this interface via PeerAgents
// (the field Crew.Agents cannot share a method name in Go).
type DelegationRoster interface {
	// PeerAgents returns the peer agents available for delegation.
	PeerAgents() []*Agent
}

// agentRoleKey carries the Role of the agent currently executing a task,
// so nested tools (especially delegation) can detect self-calls and cycles.
type agentRoleKey struct{}

// ContextWithAgentRole returns a child context that records the executing
// agent's role. The built-in executor sets this automatically.
func ContextWithAgentRole(ctx context.Context, role string) context.Context {
	if role == "" {
		return ctx
	}
	return context.WithValue(ctx, agentRoleKey{}, role)
}

func agentRoleFromCtx(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if r, ok := ctx.Value(agentRoleKey{}).(string); ok {
		return r
	}
	return ""
}

type delDepthKey struct{}
type delStackKey struct{}

func delegationDepth(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if d, ok := ctx.Value(delDepthKey{}).(int); ok {
		return d
	}
	return 0
}

func withDelegationDepth(ctx context.Context, d int) context.Context {
	return context.WithValue(ctx, delDepthKey{}, d)
}

func delegationStack(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	if s, ok := ctx.Value(delStackKey{}).([]string); ok {
		out := make([]string, len(s))
		copy(out, s)
		return out
	}
	return nil
}

func withDelegationStack(ctx context.Context, stack []string) context.Context {
	return context.WithValue(ctx, delStackKey{}, stack)
}

// NewDelegationTool returns a Tool named delegate_to_coworker that asks a
// peer agent from roster to solve a sub-question. The target must have
// AllowDelegation == true. Nested calls are bounded by
// DefaultMaxDelegationDepth and refuse cycles on the caller stack.
//
// Input is JSON:
//
//	{"coworker":"<role>","request":"<text>","context":"<optional>"}
//
// Errors are returned as tool observation text (not Go errors) so the
// model can recover, except when input parsing is attempted.
//
// Attach explicitly with agent.WithTools(NewDelegationTool(crew)), or
// set Crew.EnableDelegationTool to auto-attach during Kickoff.
func NewDelegationTool(roster DelegationRoster) Tool {
	return &delegationTool{roster: roster}
}

type delegationTool struct {
	roster DelegationRoster
}

func (d *delegationTool) Name() string { return delegationToolName }

func (d *delegationTool) Description() string {
	return "Delegate a sub-question to a coworker agent by exact role name. " +
		"Input JSON: {\"coworker\":\"<role>\",\"request\":\"<text>\",\"context\":\"<optional>\"}. " +
		"The coworker must have AllowDelegation enabled."
}

type delegationInput struct {
	Coworker string `json:"coworker"`
	Request  string `json:"request"`
	Context  string `json:"context"`
}

func (d *delegationTool) Call(ctx context.Context, input string) (string, error) {
	if d.roster == nil {
		return "Error: delegation roster is not configured.", nil
	}

	var in delegationInput
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "Error: empty delegation input; expected JSON with coworker and request.", nil
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return fmt.Sprintf("Error: invalid delegation JSON: %v", err), nil
	}
	in.Coworker = strings.TrimSpace(in.Coworker)
	in.Request = strings.TrimSpace(in.Request)
	if in.Coworker == "" || in.Request == "" {
		return "Error: delegation requires non-empty \"coworker\" and \"request\" fields.", nil
	}

	depth := delegationDepth(ctx)
	if depth >= DefaultMaxDelegationDepth {
		return fmt.Sprintf(
			"Error: delegation depth limit reached (%d). Finish the work yourself or simplify the request.",
			DefaultMaxDelegationDepth,
		), nil
	}

	caller := agentRoleFromCtx(ctx)
	stack := delegationStack(ctx)

	if caller != "" && strings.EqualFold(in.Coworker, caller) {
		return "Error: cannot delegate to yourself.", nil
	}
	for _, r := range stack {
		if strings.EqualFold(r, in.Coworker) {
			return fmt.Sprintf("Error: delegation cycle detected involving %q.", in.Coworker), nil
		}
	}

	target := findRosterAgent(d.roster.PeerAgents(), in.Coworker)
	if target == nil {
		return fmt.Sprintf(
			"Error: coworker %q not found. Available: %s.",
			in.Coworker, strings.Join(rosterRoles(d.roster.PeerAgents()), ", "),
		), nil
	}
	if !target.AllowDelegation {
		return fmt.Sprintf(
			"Error: coworker %q is not eligible for delegation (AllowDelegation=false).",
			target.Role,
		), nil
	}

	nctx := withDelegationDepth(ctx, depth+1)
	if caller != "" {
		nstack := append(stack, caller)
		nctx = withDelegationStack(nctx, nstack)
	}
	nctx = ContextWithAgentRole(nctx, target.Role)

	desc := in.Request
	if strings.TrimSpace(in.Context) != "" {
		desc = in.Request + "\n\nAdditional context:\n" + in.Context
	}
	sub := NewTask(desc, "A concise answer to the delegated request.", target)

	out, err := target.Execute(nctx, sub)
	if err != nil {
		return fmt.Sprintf("Error: coworker %q failed: %v", target.Role, err), nil
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return fmt.Sprintf("coworker %q returned an empty answer.", target.Role), nil
	}
	return truncateToolOutput(out), nil
}

func findRosterAgent(agents []*Agent, role string) *Agent {
	for _, a := range agents {
		if a == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(a.Role), role) {
			return a
		}
	}
	return nil
}

func rosterRoles(agents []*Agent) []string {
	var names []string
	for _, a := range agents {
		if a == nil || a.Role == "" {
			continue
		}
		names = append(names, a.Role)
	}
	return names
}

// hasDelegationTool reports whether tools already includes delegate_to_coworker.
func hasDelegationTool(tools []Tool) bool {
	for _, t := range tools {
		if t != nil && t.Name() == delegationToolName {
			return true
		}
	}
	return false
}
