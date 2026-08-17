# Native Tool Calling Example

Demonstrates native tool calling with a mock LLM. No API key needed.

## Run

```bash
go run ./examples/native_tools
```

## Expected Output

```
Final output: The result of 2+2 is 4.

Tool trace: calculator("2+2") -> 4 (failed=false, duration=...)
```

## How it works

1. The mock LLM is configured with two scripted responses.
2. On the first `CallWithTools`, the model requests to call the `calculator` tool.
3. The executor runs the tool and feeds the result back.
4. On the second call, the model produces the final answer.
5. `ToolTraces` in the output shows each tool invocation for auditing.