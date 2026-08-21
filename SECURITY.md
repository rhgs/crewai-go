# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| v0.8.x  | ✅                 |
| v0.7.x  | ✅                 |
| v0.6.x  | ✅                 |
| v0.5.x  | ✅                 |
| v0.4.x  | ✅                 |
| v0.3.x  | ✅                 |
| < v0.3  | ❌                 |

## Reporting a Vulnerability

If you discover a security vulnerability, **please do not open a public issue**.

Instead, report it privately:

1. Go to the [Security tab](https://github.com/rhgs/crewai-go/security) and click **"Report a vulnerability"**, or
2. Email the maintainer directly.

Please include:

- A description of the vulnerability and its impact
- Steps to reproduce
- Affected versions / commits
- Suggested fix (if any)

We will acknowledge receipt within **72 hours** and aim to provide a fix or mitigation within **7 days**, depending on severity.

## Scope

This policy applies to the `crewai-go` core package and its bundled LLM providers (`llm/openai`, `llm/anthropic`, `llm/ollama`, `llm/xai`), tools (`tools/`), and the MCP client (`mcp/`).

Issues related to third-party LLM APIs (OpenAI, Anthropic, xAI, Ollama) or third-party MCP servers should be reported to their respective providers.

## Hardening notes (operators)

- **Secrets in logs.** Debug logs include full LLM outputs and tool inputs. Keep production log level at `LevelInfo`/`LevelError` (or wrap with `crewai.RedactHandler`). Provider error messages are passed through `redactError` before being logged or surfaced via `Progress.Err`.
- **SSRF.** `WebSearchTool` filters result URLs (non-http(s), userinfo, loopback/private/link-local/unspecified/multicast/CGNAT, DNS rebinding, fail-closed). It does **not** fetch those URLs — only presents them. Do not build a follow-up "fetch URL" tool without your own allowlist.
- **MCP.** Treat remote MCP servers as trusted code (see `docs/en/mcp.md` threat model). The client defaults to a 30s HTTP timeout (`DefaultHTTPTimeout`); override via `WithHTTPTimeout`, `WithHTTPClient`, or JSON `servers[].timeout` (`"0s"` disables). Protect JSON config files that hold bearer tokens (`0600`). Prefer `FilterTools` / minimal tool attach; optional `WithDescriptionLimit` for description hygiene.
- **Output files.** `Task.OutputFile` is written with mode `0600`. Paths are cleaned; empty paths are rejected. Optional `Task.OutputDir` / `Crew.OutputDir` jails writes (symlink-aware `EvalSymlinks`, fail closed with `ErrOutputPathRejected`). Still treat paths as application-trusted — never pass unvalidated model output as `OutputFile`.
- **Memory FileStore.** `OpenFileStore(dir)` roots are **caller-trusted** — never pass model-controlled paths. v1 is single-writer per root; the app owns `Close`. Files `0600`, dirs `0700`. Corrupt JSONL lines are skipped on open (`CorruptSkipped()`).
- **Lifecycle events.** `Crew.WithEvents` / `CrewEvent` are **metadata-only** by default (no prompt bodies, tool args, or stream text). Error strings are redacted. Same concurrency rules as Progress.
- **JSON Schema `$ref`.** Only local fragment refs (`#/...`) are resolved — never HTTP/file fetches (SSRF).
- **Stream sinks.** `Crew.WithStream` / `StreamFunc` receive **raw model text** deltas (and redacted terminal errors). Unlike `Progress`, stream content is intentionally the completion itself — filter/redact in the app before exposing to untrusted multi-tenant clients. `RedactHandler` does not wrap StreamFunc. Provider stream readers cap **raw HTTP body bytes** at `MaxProviderResponseBytes` (same as non-stream).
- **Kickoff single-flight.** Concurrent `Kickoff` on the same `*Crew` returns `ErrCrewRunning` (fail fast; does not queue).
- **Native tool calling.** Argument size/depth and tool output size are capped (`MaxToolArgsBytes`, `MaxToolArgsDepth`, `MaxToolOutputBytes`). Provider response bodies are capped at `MaxProviderResponseBytes` (10 MiB).