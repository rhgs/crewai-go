// Package anthropic implements crewai.LLM for the Anthropic (Claude) Messages
// API, using only the standard library.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rhgs/crewai-go"
)

const (
	defaultBaseURL = "https://api.anthropic.com/v1"
	apiVersion     = "2023-06-01"
)

// Client talks to the Anthropic Messages API.
type Client struct {
	apiKey      string
	model       string
	baseURL     string
	maxTokens   int
	temperature float64
	httpClient  *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL sets an alternative base URL.
func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

// WithMaxTokens sets the maximum number of tokens generated per response.
func WithMaxTokens(n int) Option { return func(c *Client) { c.maxTokens = n } }

// WithTemperature adjusts the sampling temperature.
func WithTemperature(t float64) Option { return func(c *Client) { c.temperature = t } }

// WithHTTPClient injects a custom *http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithAPIKey sets the API key explicitly.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// New creates a client for the given Claude model (e.g. "claude-sonnet-5").
// If no key is passed, the ANTHROPIC_API_KEY environment variable is used.
func New(model string, opts ...Option) *Client {
	c := &Client{
		apiKey:      os.Getenv("ANTHROPIC_API_KEY"),
		model:       model,
		baseURL:     defaultBaseURL,
		maxTokens:   4096,
		temperature: 0.7,
		httpClient:  &http.Client{Timeout: 120 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Model implements crewai.LLM.
func (c *Client) Model() string { return c.model }

type messagesRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	System      string    `json:"system,omitempty"`
	Messages    []anthMsg `json:"messages"`
}

type anthMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Call implements crewai.LLM.
func (c *Client) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("anthropic: missing API key (set ANTHROPIC_API_KEY or use WithAPIKey)")
	}

	// The Anthropic API receives the system prompt in a separate field and
	// only accepts 'user' and 'assistant' messages.
	var systemParts []string
	var msgs []anthMsg
	for _, m := range messages {
		switch m.Role {
		case crewai.RoleSystem:
			systemParts = append(systemParts, m.Content)
		case crewai.RoleAssistant:
			msgs = append(msgs, anthMsg{Role: "assistant", Content: m.Content})
		default: // user and tool are mapped to user
			msgs = append(msgs, anthMsg{Role: "user", Content: m.Content})
		}
	}

	reqBody := messagesRequest{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		System:      strings.Join(systemParts, "\n\n"),
		Messages:    msgs,
	}

	buf, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("anthropic: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("anthropic: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: sending request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, crewai.MaxProviderResponseBytes))
	if err != nil {
		return "", fmt.Errorf("anthropic: reading response: %w", err)
	}

	var parsed messagesResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("anthropic: decoding response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("anthropic: API error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		// Do not echo the body: error payloads may contain request metadata.
		return "", fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode)
	}

	var b strings.Builder
	for _, blk := range parsed.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String(), nil
}

// anthToolUseBlock is a content block of type "tool_use".
type anthToolUseBlock struct {
	Type  string          `json:"type"` // "tool_use"
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"` // object, not string
}

// anthContentBlock is a union of text and tool_use blocks.
type anthContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// tool_use fields (when Type == "tool_use")
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// anthMessagesResponseWithTools extends messagesResponse with tool_use blocks.
type anthMessagesResponseWithTools struct {
	Content []anthContentBlock `json:"content"`
	Error   *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// anthToolDef is the Anthropic wire format for a client tool definition.
// Unlike OpenAI's nested ToolSpec ({type,function:{name,description,parameters}}),
// Anthropic expects a flat object with name/description/input_schema.
// See https://docs.anthropic.com/en/docs/build-with-claude/tool-use.
type anthToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// toAnthropicTools converts crewai.ToolSpec values into Anthropic's flat
// tool definition shape. Parameters become input_schema; an empty schema
// is replaced with a permissive object so the API always receives a valid
// JSON Schema object.
func toAnthropicTools(tools []crewai.ToolSpec) []anthToolDef {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthToolDef, len(tools))
	for i, t := range tools {
		schema := t.Function.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out[i] = anthToolDef{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: schema,
		}
	}
	return out
}

// anthContentPart is one content block in a request message. Anthropic
// accepts either a plain string Content or an array of typed blocks;
// we always send the array form so tool_use / tool_result round-trips
// correctly across multi-turn native tool loops.
type anthContentPart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"` // tool_result body
	IsError   bool            `json:"is_error,omitempty"`
}

// anthMessagesRequestWithTools is the body for a Messages request that
// declares client tools. Messages.Content is typed as any so it can be
// either a string (simple turns) or []anthContentPart (tool turns).
type anthMessagesRequestWithTools struct {
	Model       string        `json:"model"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	System      string        `json:"system,omitempty"`
	Messages    []anthReqMsg  `json:"messages"`
	Tools       []anthToolDef `json:"tools,omitempty"`
}

// anthReqMsg is one message in the request. Content is string for plain
// turns and []anthContentPart when tool_use / tool_result blocks are
// present.
type anthReqMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// CallWithTools implements crewai.ToolCallingLLM.
//
// Anthropic differs from OpenAI in three ways this method normalises:
//  1. Tool definitions are flat {name, description, input_schema}, not
//     nested under "function".
//  2. Model-requested tool calls arrive as content blocks of type
//     "tool_use" with an object "input" (not a JSON string).
//  3. Tool results must be returned as content blocks of type
//     "tool_result" carrying the matching tool_use_id — not as a free
//     text user message. Assistant turns that requested tools must
//     likewise be replayed as content blocks, not plain strings.
func (c *Client) CallWithTools(ctx context.Context, messages []crewai.Message, tools []crewai.ToolSpec) (*crewai.ToolCallResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("anthropic: missing API key (set ANTHROPIC_API_KEY or use WithAPIKey)")
	}

	var systemParts []string
	var msgs []anthReqMsg
	for _, m := range messages {
		switch m.Role {
		case crewai.RoleSystem:
			systemParts = append(systemParts, m.Content)
		case crewai.RoleAssistant:
			if len(m.ToolCalls) == 0 {
				msgs = append(msgs, anthReqMsg{Role: "assistant", Content: m.Content})
				continue
			}
			// Replay the prior assistant turn as typed content blocks so
			// the model can match subsequent tool_result blocks by id.
			parts := make([]anthContentPart, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				parts = append(parts, anthContentPart{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				input := tc.Function.Arguments
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				parts = append(parts, anthContentPart{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			msgs = append(msgs, anthReqMsg{Role: "assistant", Content: parts})
		case crewai.RoleTool:
			// Anthropic requires tool results as user-role content blocks
			// of type tool_result, keyed by the tool_use id from the
			// preceding assistant turn. ToolCallID carries that id
			// (populated by the executor from ToolCall.ID).
			toolUseID := m.ToolCallID
			if toolUseID == "" {
				// Backward-compatible fallback for callers that only
				// set ToolName (pre-ToolCallID). The API will reject
				// an empty tool_use_id; surface the name so the error
				// is at least attributable.
				toolUseID = m.ToolName
			}
			parts := []anthContentPart{{
				Type:      "tool_result",
				ToolUseID: toolUseID,
				Content:   m.Content,
			}}
			msgs = append(msgs, anthReqMsg{Role: "user", Content: parts})
		default: // user
			msgs = append(msgs, anthReqMsg{Role: "user", Content: m.Content})
		}
	}

	reqBody := anthMessagesRequestWithTools{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		System:      strings.Join(systemParts, "\n\n"),
		Messages:    msgs,
		Tools:       toAnthropicTools(tools),
	}

	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("anthropic: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("anthropic: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: sending request: %w", err)
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, crewai.MaxProviderResponseBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: reading response: %w", err)
	}

	var parsed anthMessagesResponseWithTools
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic: decoding response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("anthropic: API error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode)
	}

	// Extract text and tool_use blocks from the response.
	var textContent strings.Builder
	var toolCalls []crewai.ToolCall
	for _, blk := range parsed.Content {
		switch blk.Type {
		case "text":
			textContent.WriteString(blk.Text)
		case "tool_use":
			toolCalls = append(toolCalls, crewai.ToolCall{
				ID: blk.ID,
				Function: crewai.ToolCallFunction{
					Name:      blk.Name,
					Arguments: blk.Input,
				},
			})
		}
	}

	return &crewai.ToolCallResponse{
		Content:   textContent.String(),
		ToolCalls: toolCalls,
	}, nil
}

// Compile-time check.
var _ crewai.ToolCallingLLM = (*Client)(nil)

// anthWebSearchResult is a single search result inside a
// web_search_tool_result content block. The Anthropic API returns
// results as typed objects (type: "web_search_result"), not as JSON
// text inside a tool_result block.
type anthWebSearchResult struct {
	Type             string `json:"type"` // "web_search_result"
	Title            string `json:"title"`
	URL              string `json:"url"`
	EncryptedContent string `json:"encrypted_content"`
}

// anthWebSearchToolResultBlock is a content block of type
// "web_search_tool_result" in the response. This is a SERVER tool
// (not a client tool), so the type is "web_search_tool_result", NOT
// "tool_result".
type anthWebSearchToolResultBlock struct {
	Type      string                `json:"type"` // "web_search_tool_result"
	ToolUseID string                `json:"tool_use_id"`
	Content   []anthWebSearchResult `json:"content"`
}

// anthWebSearchResponse captures the response when web_search is used.
// The content array may contain text blocks, server_tool_use blocks,
// and web_search_tool_result blocks. We parse each raw block
// individually to determine its type.
type anthWebSearchResponse struct {
	Content []json.RawMessage `json:"content"`
	Error   *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// WebSearch implements crewai.WebSearcher.
//
// Anthropic does not have a standalone search API. Instead, this method
// sends a messages request with the web_search_20250305 tool enabled,
// using the query as the user message. The model searches and returns
// results as web_search_tool_result content blocks containing
// web_search_result objects with title, url, and encrypted_content.
//
// Note: this consumes tokens (the model is involved), unlike Ollama's
// pure search endpoint. For high-volume search, consider using the
// WebSearchTool with a Brave or Google provider instead.
func (c *Client) WebSearch(ctx context.Context, query string, max int) ([]crewai.SearchHit, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("anthropic: missing API key (set ANTHROPIC_API_KEY or use WithAPIKey)")
	}

	if max <= 0 || max > 10 {
		max = 5
	}

	// Build the tool spec as raw JSON to avoid coupling with the
	// existing ToolSpec type (which is for function tools).
	toolsRaw := json.RawMessage(`[{"type":"web_search_20250305","name":"web_search","max_uses":` +
		fmt.Sprintf("%d", max) + `}]`)

	reqBody := struct {
		Model     string          `json:"model"`
		MaxTokens int             `json:"max_tokens"`
		Messages  []anthMsg       `json:"messages"`
		Tools     json.RawMessage `json:"tools,omitempty"`
	}{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		Messages:  []anthMsg{{Role: "user", Content: query}},
		Tools:     toolsRaw,
	}

	buf, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("anthropic: creating web search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: sending web search request: %w", err)
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, crewai.MaxProviderResponseBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: reading web search response: %w", err)
	}

	var parsed anthWebSearchResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		// Prefer a clean status error over a decode error when the
		// server already signalled failure — never echo the body.
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("anthropic: web search HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("anthropic: decoding web search response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("anthropic: API error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: web search HTTP %d", resp.StatusCode)
	}

	// The content array has mixed types. We parse each raw block
	// individually to determine its type, then extract search results
	// from web_search_tool_result blocks.
	var hits []crewai.SearchHit
	for _, raw := range parsed.Content {
		// Peek at the type field to determine the block type.
		var typeProbe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &typeProbe); err != nil {
			continue
		}

		if typeProbe.Type != "web_search_tool_result" {
			continue
		}

		var block anthWebSearchToolResultBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			continue
		}

		for _, r := range block.Content {
			if r.Type != "web_search_result" {
				continue
			}
			hits = append(hits, crewai.SearchHit{
				Title:   r.Title,
				URL:     r.URL,
				Content: r.EncryptedContent,
			})
		}
	}

	// Clamp to max.
	if len(hits) > max {
		hits = hits[:max]
	}

	return hits, nil
}

// Compile-time check.
var _ crewai.WebSearcher = (*Client)(nil)
