package crewai

import "encoding/json"

// SchemaProvider is an optional interface a Tool may implement to
// expose its JSON Schema for native tool calling. When the executor
// builds ToolSpecs for a ToolCallingLLM, it checks each tool: if the
// tool implements SchemaProvider and returns a non-empty schema, that
// schema is used as ToolSpec.Function.Parameters. Otherwise the
// default empty object schema is used.
//
// This lets tools such as MCP adapters carry their real input schema
// forward to the model instead of a placeholder.
type SchemaProvider interface {
	// Schema returns the JSON Schema the model should see for this
	// tool's parameters, or nil if no schema is available.
	Schema() json.RawMessage
}
