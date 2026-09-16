# RAG como padrão

> **Languages:** [English](../rag.md) · **Português** (atual)

crewai-go **não** embarca um vector database. RAG é um padrão de aplicação:
persistir chunks num `MemoryStore`, embedar com `Crew.Embed` / `EmbeddingFunc`,
recuperar com `MemoryQuery.Embedding`, e wrapping de `Query` como `Tool` se o
agente deve decidir quando buscar.

Isso fecha **D-T9** (só docs na v1). Embeddings já encaixam via
`EmbeddingFunc` (M4); `FileStore` (M3) é o backend durável.

## Esboço

```go
store, _ := crewai.OpenFileStore(dir) // root confiável do caller
defer store.Close()

vecs, _ := embed(ctx, []string{chunk})
store.Put(ctx, crewai.MemoryEntry{Content: chunk, Embedding: vecs[0]})

rag := crewai.NewTool(
    "memory_query",
    "Recall semântico no store. Input: uma query curta.",
    func(ctx context.Context, q string) (string, error) {
        qv, err := embed(ctx, []string{q})
        if err != nil {
            return "", err
        }
        hits, err := store.Query(ctx, crewai.MemoryQuery{Embedding: qv[0], Limit: 5})
        if err != nil {
            return "", err
        }
        var b strings.Builder
        for i, h := range hits {
            fmt.Fprintf(&b, "%d. %s\n", i+1, h.Content)
        }
        return b.String(), nil
    },
)
agente.WithTools(rag)
```

## O que fica fora do core

- Chunking / parsers de PDF / tokenizers
- Vector DBs remotos (Pinecone, Qdrant, clientes pgvector)
- Um tipo `MemoryQueryTool` embutido — copie o esboço (ou `examples/rag_file`)

## Relacionado

- [`docs/pt-BR/memory.md`](memory.md) — `MemoryStore`, `FileStore`, `AutoEmbed`
- `examples/rag_file` — FileStore + embedder bag-of-words, 100% offline
- `examples/memory_embed` — AutoEmbed na barreira de commit
