# RAG as a pattern

> **Languages:** **English** (current) · [Português](pt-BR/rag.md)

crewai-go does **not** ship a vector database. Retrieval-augmented generation
is an application pattern: persist chunks in a `MemoryStore`, embed with
`Crew.Embed` / `EmbeddingFunc`, recall with `MemoryQuery.Embedding`, and wrap
`Query` as a `Tool` if the agent should decide when to search.

This matches **D-T9** (docs-only v1). Embeddings already hook via
`EmbeddingFunc` (M4); `FileStore` (M3) is the durable backend.

## Sketch

```go
store, _ := crewai.OpenFileStore(dir) // caller-trusted root
defer store.Close()

// Seed (offline or at ingest time).
vecs, _ := embed(ctx, []string{chunk})
store.Put(ctx, crewai.MemoryEntry{Content: chunk, Embedding: vecs[0]})

rag := crewai.NewTool(
    "memory_query",
    "Semantic recall over the document store. Input: a short query.",
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
agent.WithTools(rag)
```

## What stays out of core

- Chunking / PDF parsers / tokenizers
- Remote vector DBs (Pinecone, Qdrant, pgvector clients)
- A built-in `MemoryQueryTool` type — copy the sketch (or `examples/rag_file`)

## Related

- [`docs/memory.md`](memory.md) — `MemoryStore`, `FileStore`, `AutoEmbed`
- `examples/rag_file` — FileStore + bag-of-words embedder, fully offline
- `examples/memory_embed` — AutoEmbed at the commit barrier
