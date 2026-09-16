package crewai

// crewConfigSchema validates the declarative JSON-subset document (D-Y4).
const crewConfigSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["agents", "tasks"],
  "properties": {
    "agents": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["role"],
        "properties": {
          "name": {"type": "string"},
          "role": {"type": "string", "minLength": 1},
          "goal": {"type": "string"},
          "backstory": {"type": "string"},
          "llm": {"type": "string"},
          "tools": {"type": "array", "items": {"type": "string"}},
          "max_iterations": {"type": "integer", "minimum": 0},
          "allow_delegation": {"type": "boolean"},
          "tool_mode": {"type": "string"}
        }
      }
    },
    "tasks": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["description"],
        "properties": {
          "name": {"type": "string"},
          "description": {"type": "string", "minLength": 1},
          "expected_output": {"type": "string"},
          "agent": {"type": "string"},
          "tools": {"type": "array", "items": {"type": "string"}},
          "context": {
            "type": "array",
            "items": {
              "oneOf": [
                {"type": "string"},
                {"type": "integer", "minimum": 0}
              ]
            }
          },
          "async": {"type": "boolean"},
          "output_file": {"type": "string"},
          "guardrail": {"type": "string"}
        }
      }
    },
    "crew": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "name": {"type": "string"},
        "process": {"type": "string"},
        "verbose": {"type": "boolean"},
        "memory": {"type": "boolean"},
        "manager_llm": {"type": "string"},
        "manager_agent": {"type": "string"},
        "output_dir": {"type": "string"},
        "enable_delegation_tool": {"type": "boolean"},
        "async_max_workers": {"type": "integer", "minimum": 0},
        "async_fail_fast": {"type": "boolean"},
        "guardrails": {"type": "array", "items": {"type": "string"}}
      }
    }
  }
}`
