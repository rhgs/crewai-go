# Contribuindo para o crewai-go

> **Languages:** [English](CONTRIBUTING.md) · **Português** (atual)

Obrigado pelo interesse em contribuir. Este projeto busca permanecer pequeno,
auditavel e sem dependencias externas, sem deixar de ser util para cargas
multi-agente reais.

## Codigo de Conduta

Ao participar, voce concorda em seguir o nosso
[Codigo de Conduta](CODE_OF_CONDUCT.pt-BR.md) ([EN](CODE_OF_CONDUCT.md)).

## Formas de contribuir

- Reportar bugs e pedir features via [GitHub Issues](https://github.com/rhgs/crewai-go/issues)
- Melhorar a documentacao (espelhos em ingles e portugues)
- Adicionar exemplos, testes ou features pequenas e focadas
- Revisar pull requests

Abra uma issue antes de mudancas grandes de design para alinharmos o escopo.

## Ambiente de desenvolvimento

Requisitos:

- Go **1.24+**
- `git`

```bash
git clone https://github.com/rhgs/crewai-go.git
cd crewai-go
go test ./...
```

Nao ha dependencias externas de modulo (`go.mod` e apenas stdlib).

## Convencoes do projeto

| Regra | Detalhe |
|------|---------|
| Zero dependencias externas | Nao adicione entradas ao `go.mod` sem discussao e justificativa. |
| Ingles no codigo | Godoc, identificadores, mensagens de erro e prompts em ingles (sem acentos). |
| Docs bilingues | Docs de usuario (`README`, `docs/`, planos, changelogs) mantem espelhos EN + PT-BR. |
| Sentinel errors | Prefira `var Err… = errors.New(...)` no pacote e `errors.Is`. |
| Context em I/O | Propague `context.Context` em toda chamada LLM, tool e rede. |
| Opcoes funcionais | Prefira opcoes `WithX(...)` para configuracao opcional. |
| Testes hermeticos | Use `llm/mock` e fakes locais; sem rede real em testes unitarios. |
| Seguranca de race | Provedores e tipos compartilhados devem ser seguros para uso concorrente; o CI roda `-race`. |

## Fluxo de trabalho

1. Faca um fork do repositorio (ou crie uma branch se tiver acesso de escrita).
2. Crie uma branch focada: `feat/…`, `fix/…`, `docs/…`.
3. Faca commits pequenos e faceis de revisar.
4. Mantenha o comportamento padrao backward compatible, salvo se o PR for uma quebra intencional.
5. Atualize docs e CHANGELOG quando o comportamento visivel ao usuario mudar.
6. Abra um pull request contra `main` e preencha o template do PR.

### Checagens locais (devem passar)

```bash
gofmt -l .
go vet ./...
go build ./...
go test -race ./...
go test -cover ./...
```

O CI roda os mesmos portoes em todo pull request.

O hook de pre-commit versionado tambem roda `gofmt` nos arquivos Go
staged e `govulncheck ./...` (toolchain Go 1.25.x, para CVEs da
stdlib ja corrigidas nao falharem o commit). Instale uma vez por clone:

```bash
ln -sf ../../scripts/pre-commit .git/hooks/pre-commit
```

### Cobertura

O CI mede cobertura de statements apenas nos pacotes de biblioteca
(`go list ./...` excluindo `/examples`). O portao do projeto e **≥ 90%** no
total. Codigo novo deve vir com testes. Prefira testes table-driven e o mock
LLM para fluxos multi-fase de agentes.

## Checklist do pull request

- [ ] `gofmt`, `go vet`, `go build` e `go test -race` limpos
- [ ] Testes cobrem o novo comportamento (incluindo caminhos de falha)
- [ ] Docs atualizados (EN + PT-BR quando aplicavel)
- [ ] Entrada no CHANGELOG na secao unreleased / proxima versao quando relevante
- [ ] Sem segredos, API keys ou credenciais no diff
- [ ] A descricao do PR explica o *porquê*, nao so o *o que*

## Reportar problemas de seguranca

**Nao** abra uma issue publica para vulnerabilidades. Siga o [SECURITY.md](SECURITY.md).

## Licenca

Ao contribuir, voce concorda que suas contribuicoes serao licenciadas sob a
[Licenca MIT](LICENSE) que cobre este projeto.
