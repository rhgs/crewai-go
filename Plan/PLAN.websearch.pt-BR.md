# Plano — Tool de WebSearch: Busca Web via HTTP

> **Languages:** [English](PLAN.websearch.md) · **Português** (atual)

> **Status:** Rascunho para implementação
> **Escopo:** Adicionar uma tool `WebSearch` embutida no pacote `tools` que
> permite aos agentes buscar na web durante o loop ReAct. A tool implementa
> `FactSource` para que os resultados sejam coletados como `Fact`s com
> proveniencia. Zero novas dependencias (apenas stdlib `net/http`,
> `encoding/json`, `net/url`).

---

## 1. Objetivos e Restricoes

### 1.1 Objetivos

1. Fornecer uma tool `WebSearch` no pacote `tools` que agentes podem usar para
   buscar na web por informacoes em tempo real durante o loop ReAct.
2. Suportar backends de busca plugaveis via uma interface `SearchProvider`, para
   que usuarios possam conectar qualquer API de busca (Google Custom Search,
   Bing, Brave Search, SearXNG, DuckDuckGo, etc.) sem modificar o framework.
3. Fornecer um provedor padrao que funciona **sem chave de API** — usando o
   endpoint HTML do DuckDuckGo — para que a tool funcione out-of-the-box para
   experimentacao rapida.
4. Implementar `FactSource` para que resultados de busca sejam coletados
   automaticamente como `Fact`s com proveniencia completa (org fonte, URL,
   timestamp, hash do payload).
5. Suportar limitacao de resultados (`MaxResults`), extracao de snippets e
   normalizacao de URLs.
6. Ser seguro por padrao: timeout, tamanho maximo de resposta, e sem SSRF
   (sem IPs internos ou localhost nos resultados).

### 1.2 Restricoes (devem ser preservadas)

| Restricao | Aplicacao |
|---|---|
| Zero dependencias externas | `go.mod` inalterado; usa apenas `net/http`, `encoding/json`, `net/url`, `strings`, `time`, `fmt`, `crypto/sha256`. |
| LLM-agnostico | A tool e uma `Tool` + `FactSource`; roda durante o loop ReAct. Sem dependencia de LLM. |
| Comentarios em ingles, sem acentos | Todo godoc, mensagens de erro, identificadores. |
| Testes hermeticos | Usar `httptest.NewServer` para simular a API de busca; sem rede real. |
| Sentinel errors, context em todo I/O, opcoes funcionais | Novas sentinelas em `errors.go` (ou locais em `tools`); `ctx` passado a todas as chamadas HTTP; opcoes funcionais para `WebSearch`. |
| Seguranca de concorrencia | Implementacoes de `SearchProvider` devem ser seguras para uso concorrente (stateless ou mutex-protegidas). |
| Backward compatibility | A tool e opt-in; sem mudancas em caminhos de codigo existentes. A integracao de `FactSource` ja esta no executor. |
| Protecao SSRF | Bloquear requisicoes para IPs privados/loopback e esquemas nao-HTTP(S). |

---

## 2. Nova superficie de API

### 2.1 Interface `SearchProvider` (pacote tools)

```go
// SearchProvider e a interface para um backend de busca web. Implementacoes
// devem ser seguras para uso concorrente.
type SearchProvider interface {
    // Search consulta o backend e retorna resultados de busca.
    Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
    // Name identifica o provedor (ex. "duckduckgo", "google", "brave").
    Name() string
}
```

### 2.2 Struct `SearchResult`

```go
// SearchResult representa um unico resultado de busca de um SearchProvider.
type SearchResult struct {
    // Title e o titulo da pagina resultante.
    Title string `json:"title"`
    // URL e o link para a pagina resultante.
    URL string `json:"url"`
    // Snippet e um trecho de texto curto do resultado.
    Snippet string `json:"snippet"`
    // SourceOrg e o nome da organizacao/site, usado para proveniencia de Fact.
    SourceOrg string `json:"source_org,omitempty"`
}
```

### 2.3 Tool `WebSearch`

```go
// WebSearchTool e uma Tool que busca na web via um SearchProvider. Tambem
// implementa FactSource para que os resultados sejam coletados como Facts com
// proveniencia.
type WebSearchTool struct {
    provider SearchProvider
    maxResults int
    timeout time.Duration

    mu sync.Mutex
    lastFacts []crewai.Fact
}
```

### 2.4 Opcoes funcionais

```go
// WithMaxResults define o numero maximo de resultados a retornar.
func WithMaxResults(n int) func(*WebSearchTool)

// WithSearchTimeout define o timeout HTTP para requisicoes de busca.
func WithSearchTimeout(d time.Duration) func(*WebSearchTool)
```

### 2.5 Construtor

```go
// NewWebSearch cria uma tool de busca web com o provedor e opcoes fornecidos.
// Se provider for nil, o padrao (DuckDuckGo) e usado.
func NewWebSearch(provider SearchProvider, opts ...func(*WebSearchTool)) *WebSearchTool

// NewWebSearchWithDefault cria uma tool de busca web usando o provedor padrao
// DuckDuckGo (sem chave de API).
func NewWebSearchWithDefault(opts ...func(*WebSearchTool)) *WebSearchTool
```

### 2.6 Provedores embutidos

| Provedor | Pacote | Auth | Status |
|----------|--------|------|--------|
| DuckDuckGo (HTML) | `tools` (`defaultSearchProvider`) | nenhuma | Padrao, sem chave |
| Google Custom Search | `tools` | `GOOGLE_API_KEY` + `GOOGLE_CSE_ID` | Opcional, via `NewGoogleSearch` |
| Brave Search | `tools` | `BRAVE_API_KEY` | Opcional, via `NewBraveSearch` |

### 2.7 Integracao com Facts

`WebSearchTool` implementa `FactSource`. Apos uma `Call` bem-sucedida, converte
cada `SearchResult` em um `Fact`:

```
Fact{
    Claim:       result.Title + ": " + result.Snippet,
    SourceOrg:   result.SourceOrg (ou hostname do result.URL),
    SourceURL:   result.URL,
    CollectedAt: time.Now(),
    PayloadHash: sha256(rawJSONPayload),
}
```

O payload bruto para hashing e a codificacao JSON do `SearchResult`.

---

## 3. Provedor padrao DuckDuckGo

### 3.1 Por que DuckDuckGo?

- Sem chave de API — funciona out-of-the-box.
- Endpoint HTML (`https://duckduckgo.com/html/`) e estavel e parseavel.
- Sem limite de taxa para uso de baixo volume (aceitavel para demos de agentes).

### 3.2 Implementacao

```
GET https://duckduckgo.com/html/?q={query}

- Parsear resposta HTML usando regex (sem parser HTML externo).
- Extrair blocos de resultado: title, URL, snippet.
- O HTML do DuckDuckGo usa:
  <a class="result__a" href="...">Title</a>
  <a class="result__snippet" ...>Snippet</a>
- Limitar a MaxResults.
- Normalizacao de URL: DuckDuckGo redireciona via //duckduckgo.com/l/?uddg=;
  decodificar a URL real.
```

### 3.3 Parse via regex (sem dependencia externa)

```go
var (
    // resultLink corresponde a tags anchor de resultado.
    resultLink = regexp.MustCompile(`<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
    // resultSnippet corresponde a elementos de snippet.
    resultSnippet = regexp.MustCompile(`<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
    // stripTags remove tags HTML de uma string.
    stripTags = regexp.MustCompile(`<[^>]*>`)
    // unescapeEntities decodifica entidades HTML basicas.
    unescapeEntities = html.UnescapeString
)
```

Isso usa apenas `regexp` e `html` (ambos stdlib). O parse e intencionalmente
simples e tolerante — o HTML do DuckDuckGo pode mudar, e a tool degrada
graciosamente (retorna menos resultados, nao um erro).

### 3.4 Protecao SSRF

```go
// isBlockedURL verifica se uma URL aponta para um endereco privado/loopback.
func isBlockedURL(rawURL string) bool {
    u, err := url.Parse(rawURL)
    if err != nil {
        return true
    }
    if u.Scheme != "http" && u.Scheme != "https" {
        return true
    }
    host := u.Hostname()
    if host == "localhost" || host == "127.0.0.1" || host == "::1" {
        return true
    }
    // Bloquear ranges privados (10.x, 172.16-31.x, 192.168.x, 169.254.x).
    ip := net.ParseIP(host)
    if ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
        return true
    }
    return false
}
```

---

## 4. Provedor Google Custom Search (opcional)

```go
// GoogleSearchProvider implementa SearchProvider usando a Google Custom
// Search JSON API.
type GoogleSearchProvider struct {
    apiKey string
    cxID   string // Custom Search Engine ID
}

// NewGoogleSearch cria um provedor Google Custom Search.
// Le GOOGLE_API_KEY e GOOGLE_CSE_ID do ambiente se nao definidos.
func NewGoogleSearch(apiKey, cxID string) *GoogleSearchProvider
```

Chamada de API:

```
GET https://www.googleapis.com/customsearch/v1?q={query}&key={key}&cx={cx}&num={maxResults}
```

A resposta e JSON; parsear `items[].{title, link, snippet}`.

---

## 5. Provedor Brave Search (opcional)

```go
// BraveSearchProvider implementa SearchProvider usando a Brave Search API.
type BraveSearchProvider struct {
    apiKey string
}

// NewBraveSearch cria um provedor Brave Search.
// Le BRAVE_API_KEY do ambiente se nao definido.
func NewBraveSearch(apiKey string) *BraveSearchProvider
```

Chamada de API:

```
GET https://api.search.brave.com/res/v1/web/search?q={query}&count={maxResults}
Header: X-Subscription-Token: {apiKey}
```

A resposta e JSON; parsear `web.results[].{title, url, description}`.

---

## 6. Fluxo de Execucao

### 6.1 `WebSearchTool.Call`

```
1. Parsear input como consulta de busca (trim whitespace).
2. Chamar provider.Search(ctx, query, maxResults).
3. Formatar resultados como string legivel para a observacao do ReAct:
   "Found N results for '{query}':
   1. {title} — {url}
      {snippet}
   2. ..."
4. Converter resultados em Facts e armazenar para FactSource.Facts().
5. Retornar string formatada.
```

### 6.2 Formatacao de resultados (para o agente)

```
Found 3 results for 'Go programming language':

1. The Go Programming Language — https://go.dev
   Go is an open source programming language that makes it easy to build
   simple, reliable, and efficient software.

2. Tutorial: Get started with Go — https://go.dev/doc/tutorial/getting-started
   ...

3. Go (programming language) - Wikipedia — https://en.wikipedia.org/wiki/Go_(programming_language)
   ...
```

### 6.3 Producao de Facts

Apos `Call` retornar, `Facts()` retorna o slice `[]Fact`. O executor ja chama
`Facts()` em tools que implementam `FactSource` apos uma chamada bem-sucedida
(veja `executor.go`). Nao sao necessarias mudancas no executor.

---

## 7. Mudancas em Arquivos

| Arquivo | Mudanca | Descricao |
|---------|---------|-----------|
| `tools/websearch.go` | **Novo** | Interface `SearchProvider`, `SearchResult`, `WebSearchTool`, `NewWebSearch`, `NewWebSearchWithDefault`, provedor DuckDuckGo, protecao SSRF, producao de facts. |
| `tools/websearch_test.go` | **Novo** | Testes hermeticos usando `httptest.NewServer`: busca basica, max resultados, timeout, resultados vazios, SSRF bloqueado, producao de facts, interface FactSource, parse HTML DuckDuckGo, provedor Google (httptest), provedor Brave (httptest). |
| `tools/google_search.go` | **Novo** | `GoogleSearchProvider` + `NewGoogleSearch`. |
| `tools/google_search_test.go` | **Novo** | Testes usando `httptest.NewServer`. |
| `tools/brave_search.go` | **Novo** | `BraveSearchProvider` + `NewBraveSearch`. |
| `tools/brave_search_test.go` | **Novo** | Testes usando `httptest.NewServer`. |
| `tools/doc.go` | **Novo** ou **Modificado** | Doc do pacote listando todas as tools embutidas incluindo WebSearch. |
| `errors.go` | **Modificado** | Adicionar sentinelas `ErrSearchTimeout`, `ErrSearchProvider` (ou manter no pacote `tools`). |
| `examples/websearch/main.go` | **Novo** | Exemplo executavel (usa DuckDuckGo padrao; modo offline com mock para CI). |
| `docs/tools.md` | **Modificado** | Adicionar secao WebSearch. |
| `docs/pt-BR/tools.md` | **Modificado** | Espelho em portugues. |
| `PLAN.md` | **Modificado** | Atualizar roadmap: marcar web search como em andamento/concluido. |
| `PLAN.pt-BR.md` | **Modificado** | Espelho em portugues. |
| `README.md` | **Modificado** | Adicionar WebSearch a lista de tools embutidas + "What's new". |
| `README.pt-BR.md` | **Modificado** | Espelho em portugues. |
| `CHANGELOG.md` | **Modificado** | Adicionar entrada sob `[Unreleased]`. |
| `CHANGELOG.pt-BR.md` | **Modificado** | Espelho em portugues. |

---

## 8. Plano de Testes

Todos os testes usam `httptest.NewServer` — sem rede real.

### 8.1 Testes do provedor DuckDuckGo

| Teste | Descricao |
|-------|-----------|
| `TestDuckDuckGo_BasicSearch` | Mock retorna HTML com 3 resultados → parse → 3 `SearchResult` com title, URL, snippet. |
| `TestDuckDuckGo_EmptyResults` | Mock retorna HTML sem resultados → slice vazio, sem erro. |
| `TestDuckDuckGo_MaxResults` | Mock retorna 10 resultados, MaxResults=3 → apenas 3 retornados. |
| `TestDuckDuckGo_URLDecoding` | URLs de redirecionamento DuckDuckGo (`//duckduckgo.com/l/?uddg=...`) sao decodificadas para o destino real. |
| `TestDuckDuckGo_MalformedHTML` | Mock retorna HTML quebrado → sem panic, degradacao graciosa. |
| `TestDuckDuckGo_SSFRBlocked` | Resultados contendo `http://localhost:8080` ou `http://10.0.0.1` sao filtrados. |

### 8.2 Testes do provedor Google

| Teste | Descricao |
|-------|-----------|
| `TestGoogleSearch_BasicSearch` | Mock retorna JSON com items → parseados corretamente. |
| `TestGoogleSearch_APIError` | Mock retorna 403 → erro com codigo de status. |
| `TestGoogleSearch_EmptyResults` | Mock retorna `{"items": []}` → slice vazio, sem erro. |
| `TestGoogleSearch_MissingKey` | Chave de API vazia → erro. |

### 8.3 Testes do provedor Brave

| Teste | Descricao |
|-------|-----------|
| `TestBraveSearch_BasicSearch` | Mock retorna JSON com web.results → parseados corretamente. |
| `TestBraveSearch_APIError` | Mock retorna 401 → erro. |
| `TestBraveSearch_EmptyResults` | Mock retorna array de resultados vazio → slice vazio. |
| `TestBraveSearch_MissingKey` | Chave de API vazia → erro. |

### 8.4 Testes da WebSearchTool

| Teste | Descricao |
|-------|-----------|
| `TestWebSearchTool_Call` | Provedor mock retorna resultados → `Call` retorna string formatada. |
| `TestWebSearchTool_FactSource` | Apos `Call`, `Facts()` retorna Facts com Claim, SourceOrg, SourceURL, PayloadHash corretos. |
| `TestWebSearchTool_Timeout` | Provedor mock dorme > timeout → `ErrSearchTimeout`. |
| `TestWebSearchTool_EmptyQuery` | Input vazio → erro ou resultados vazios. |
| `TestWebSearchTool_FactDedup` | Duas buscas com mesmo payload → Facts deduplicados por PayloadHash (testado via executor). |
| `TestWebSearchTool_NoSSRF` | Provedor retorna resultados com IPs internos → filtrados da saida e dos facts. |

### 8.5 Exemplo

`examples/websearch/main.go`:

- Usa o provedor padrao DuckDuckGo (sem chave de API).
- Para CI: detecta env var `CI` e usa um mock server.
- Mostra um agente pesquisador que usa tools WebSearch + Calculator.

---

## 9. Ordem de Implementacao

1. **`tools/websearch.go`** — interface `SearchProvider`, `SearchResult`,
   `WebSearchTool`, `NewWebSearch`, `NewWebSearchWithDefault`, provedor
   DuckDuckGo, protecao SSRF, producao de facts, formatacao.
2. **`tools/websearch_test.go`** — todos os testes DuckDuckGo + WebSearchTool.
3. **`tools/google_search.go`** — provedor Google.
4. **`tools/google_search_test.go`** — testes Google.
5. **`tools/brave_search.go`** — provedor Brave.
6. **`tools/brave_search_test.go`** — testes Brave.
7. **`examples/websearch/main.go`** — exemplo executavel.
8. **`docs/tools.md`** + **`docs/pt-BR/tools.md`** — documentacao.
9. **`PLAN.md`** + **`PLAN.pt-BR.md`** — atualizar roadmap.
10. **`README.md`** + **`README.pt-BR.md`** — lista de features.
11. **`CHANGELOG.md`** + **`CHANGELOG.pt-BR.md`** — adicionar entrada.

---

## 10. Decisoes em Aberto

| Decisao | Opcoes | Recomendacao |
|---------|--------|-------------|
| DuckDuckGo HTML vs Lite vs API JSON? | `/html/` (parse HTML) vs `lite.duckduckgo.com` vs endpoint API | `/html/` — mais estavel, sem chave, parseavel com regex. |
| Resultados devem incluir o conteudo completo da pagina (fetch + extracao)? | Apenas snippets vs fetch completo | Apenas snippets na v1 — fetch completo e uma tool `FetchURL` separada (futuro). Mantem a tool rapida e simples. |
| `WebSearchTool` deve cachear resultados? | Sem cache vs cache em memoria com TTL | Sem cache na v1 — o loop do agente naturalmente lida com re-busca. Cache adiciona complexidade e risco de dados obsoletos. |
| A protecao SSRF deve bloquear todos os IPs nao-publicos ou apenas loopback? | Apenas loopback vs todos os ranges privados | Todos os ranges privados (10.x, 172.16-31.x, 192.168.x, 169.254.x, loopback, link-local) — defesa em profundidade. |
| A tool deve suportar operadores `site:` / `filetype:`? | Pass-through ao provedor vs filtragem | Pass-through — a string de consulta e encaminhada as-is ao provedor, que trata os operadores. |
| Onde devem ficar os erros especificos de busca? | `errors.go` raiz vs pacote `tools` | Pacote `tools` — os erros sao especificos da tool, nao do nucleo. Exportados como `tools.ErrSearchTimeout`, etc. |