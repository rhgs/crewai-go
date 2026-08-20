# Suporte a MCP (Model Context Protocol)

O crewai-go pode se conectar a servidores externos do
[Model Context Protocol](https://modelcontextprotocol.io/) sobre Streamable
HTTP (JSON-RPC 2.0) e expor o catalogo de ferramentas do servidor como
valores `crewai.Tool`. O `inputSchema` original de cada ferramenta e
preservado, para que o native function calling receba o schema real em vez de
um placeholder.

O pacote `mcp/` e padrao Go apenas (sem novas dependencias).

## Duas formas de configurar servidores

Ambas as formas exigem que o chamador forneca os detalhes do endpoint
programaticamente — o framework nunca procura arquivos de configuracao
implicitamente.

### Forma A — construcao programatica

```go
client := mcp.New("https://mcp.example.com/sse",
    mcp.WithHeader("Authorization", "Bearer "+token),
)
if err := client.Initialize(ctx, "meu-auditor", "1.0.0"); err != nil {
    return err
}
tools, err := client.ListTools(ctx)
var crewTools []crewai.Tool
for _, t := range tools {
    crewTools = append(crewTools, mcp.NewToolAdapter(client, t))
}
```

### Forma B — arquivo de configuracao JSON

`mcp.LoadConfig` le um arquivo JSON, cria um cliente por entrada de servidor
e os inicializa. O caminho do arquivo e fornecido pelo chamador. O campo opcional
`timeout` por servidor e uma duration string do Go (`"30s"`, `"1m"`,
`"0s"`). Omitido usa `DefaultHTTPTimeout` (30s); `"0s"` desliga o
deadline do client.


```json
{
  "servers": [
    {
      "name": "dados-contratos",
      "endpoint": "https://mcp.example.com/sse",
      "headers": { "Authorization": "Bearer TOKEN_AQUI" },
      "timeout": "30s"
    },
    {
      "name": "db-compras",
      "endpoint": "https://mcp2.example.com/sse",
      "headers": {},
      "timeout": "60s"
    }
  ]
}
```

```go
clients, err := mcp.LoadConfig(ctx, "/etc/auditor/mcp-servers.json",
    "meu-auditor", "1.0.0")
if err != nil { return err }
var crewTools []crewai.Tool
for _, c := range clients {
    tools, err := c.ListTools(ctx)
    if err != nil { return err }
    // Filtro opcional least-privilege (deny-by-default para nomes nao listados):
    // tools = mcp.FilterTools(tools, map[string]struct{}{"search_docs": {}, "get_ticket": {}})
    for _, t := range tools {
        crewTools = append(crewTools, mcp.NewToolAdapter(c, t,
            mcp.WithDescriptionLimit(500), // opcional; 0 = inalterado
        ))
    }
}
```

O arquivo de configuracao pode conter segredos; proteja-o com permissao `0600`.

## Comportamento do adapter

`mcp.NewToolAdapter` retorna um `*ToolAdapter` que implementa tanto
`crewai.Tool` quanto (opcionalmente) `crewai.SchemaProvider`:

- `Name()` retorna o nome da ferramenta MCP.
- `Description()` retorna a descricao da ferramenta MCP.
- `Schema()` retorna o JSON Schema de `inputSchema` — preservado literalmente
  para o native tool calling.
- `Call(ctx, input)` invoca `tools/call` e concatena o texto de cada bloco
  `content` cujo `Type` e `"text"`.

**Erros da ferramenta vs erros de protocolo.** Uma resposta com `isError: true`
NAO e um erro de Go: o adapter retorna o conteudo textual (prefixado com
`[tool error]`) para que o modelo possa observar e se recuperar. Apenas
falhas HTTP ou JSON-RPC viram erros de Go.

## Opcoes

- `mcp.WithHTTPClient(*http.Client)` — usa um client custom como esta (incl. `Timeout: 0` para desligar o deadline do client).
- `mcp.WithHTTPTimeout(time.Duration)` — define o timeout do client default da lib (ignorado se `WithHTTPClient` foi setado). Nao-positivo desliga o deadline do client.
- `mcp.WithHeader(key, val)` — adicionado a cada requisicao. Valores de
  header nunca sao logados.
- `mcp.WithDescriptionLimit(n int)` — opcao de `NewToolAdapter`; quando
  `n > 0`, remove controles ASCII da description e trunca em `n` runes
  (higiene de prompt, nao e sanitizer anti-jailbreak).
- `mcp.FilterTools(tools, allow)` — mantem so tools cujos nomes estao em
  `allow` (deny-by-default ao filtrar). Prefira isso a anexar o catalogo
  inteiro de `ListTools` a um agent.

## Seguranca

- Todos os corpos de resposta sao lidos com `io.LimitReader` (limite de
  16 MiB, `MaxMCPResponseBytes`). A paginacao de `tools/list` e limitada
  a 256 paginas.
- Headers configurados via `WithHeader` e o `Mcp-Session-Id` nunca sao logados.
- O parser de SSE extrai apenas linhas `data:`; nada e interpretado ou executado.
- Erros de `LoadConfig` nomeiam o servidor que falhou (por `Name`), mas nunca
  incluem valores de header ou conteudo do arquivo. Se um servidor posterior
  falha no `Initialize`, os clients ja abertos sao fechados com `Close`
  para nao deixar sessoes penduradas.
- `Client.Close` envia HTTP DELETE com `Mcp-Session-Id` (teardown do
  Streamable HTTP) e e idempotente.
- O client HTTP padrao usa `DefaultHTTPTimeout` (30s). Sobrescreva com
  `WithHTTPTimeout`, um `WithHTTPClient` custom, ou o campo JSON
  `"timeout"` por servidor (`"0s"` desliga o deadline do client). Prefira
  tambem um deadline explicito no `context`.
- Trate endpoints MCP como **confiaveis** — veja [Modelo de ameaca](#modelo-de-ameaca).


## Modelo de ameaca

Servidores MCP sao tratados como **codigo confiavel**, equivalente a um
binario local de tool que voce escolheu rodar. A lib nao faz sandbox de
descricoes ou resultados de tools antes de entrarem no prompt do LLM.

| Ameaca | Impacto | Mitigacoes |
|---|---|---|
| **Description** maliciosa de tool | Prompt injection / jailbreak do agent | Anexe so as tools que cada agent precisa; prefira endpoints privados/conhecidos |
| **Result** malicioso de tool | Exfiltracao de contexto previo em turnos seguintes | Catalogo least-privilege; allowlist de rede no deploy |
| Respostas travadas ou enormes | DoS / pressao de memoria | Timeout HTTP default 30s; cap de body 16 MiB; cap de paginas em `tools/list` |
| Tokens de config roubados | Abuso de auth | Arquivos de config modo `0600`; headers nunca logados |

### Checklist do operador

1. Prefira TLS e rede privada para endpoints MCP.
2. Mantenha o timeout HTTP default (30s) ou defina `timeout` / `WithHTTPTimeout`.
3. Passe sempre um deadline no `context` alem do timeout do client.
4. Proteja arquivos JSON com bearer tokens (`0600`).
5. Restrinja as tools de cada agent ao minimo — use `FilterTools` e nao anexe o catalogo inteiro.
6. Opcionalmente limite o tamanho da description com `WithDescriptionLimit`.
7. Nao logue headers MCP ou session ids (o client ja evita isso).

## Cobertura

Os arquivos `client_test.go`, `config_test.go` e `adapter_test.go` rodam
com `go test -race ./mcp/...` e cobrem mais de 90% das sentencas.
