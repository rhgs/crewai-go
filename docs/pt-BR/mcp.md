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
e os inicializa. O caminho do arquivo e fornecido pelo chamador.

```json
{
  "servers": [
    {
      "name": "dados-contratos",
      "endpoint": "https://mcp.example.com/sse",
      "headers": { "Authorization": "Bearer TOKEN_AQUI" }
    },
    {
      "name": "db-compras",
      "endpoint": "https://mcp2.example.com/sse",
      "headers": {}
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
    for _, t := range tools {
        crewTools = append(crewTools, mcp.NewToolAdapter(c, t))
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

- `mcp.WithHTTPClient(*http.Client)` — padrao `http.DefaultClient`.
- `mcp.WithHeader(key, val)` — adicionado a cada requisicao. Valores de
  header nunca sao logados.

## Seguranca

- Todos os corpos de resposta sao lidos com `io.LimitReader` (limite de 16 MiB).
- Headers configurados via `WithHeader` e o `Mcp-Session-Id` nunca sao logados.
- O parser de SSE extrai apenas linhas `data:`; nada e interpretado ou executado.
- Erros de `LoadConfig` nomeiam o servidor que falhou (por `Name`), mas nunca
  incluem valores de header ou conteudo do arquivo.

## Cobertura

Os arquivos `client_test.go`, `config_test.go` e `adapter_test.go` rodam
com `go test -race ./mcp/...` e cobrem mais de 90% das sentencas.
