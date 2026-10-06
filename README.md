# backend-go

Implementação de um teste técnico em Go para processar operações financeiras de provedores de jogos.

O serviço será composto por uma API HTTP e um consumidor SQS que usam o mesmo fluxo de negócio. O foco é preservar correção financeira perante concorrência, mensagens repetidas, reinício de processos e indisponibilidade temporária de dependências.

## Princípios

- valores monetários usam `int64` em centavos, nunca ponto flutuante;
- o saldo de uma carteira não pode ficar negativo;
- operações repetidas não podem produzir novo efeito financeiro;
- o ledger é auditável e append-only;
- PostgreSQL será a fonte de verdade para transações, locks e idempotência;
- eventos externos serão publicados por transactional outbox após o commit.

## Estado atual

O projeto possui bootstrap HTTP com Uber Fx, health checks e domínio inicial para dinheiro, carteira, transação e ledger. As integrações com PostgreSQL, SQS e OIDC serão adicionadas nas próximas etapas.

## Execução atual

Copie o arquivo de exemplo de ambiente e ajuste os valores locais:

```bash
cp .env.example .env
set -a
source .env
set +a
go run ./cmd/server
```

Health checks disponíveis:

```text
GET /health/live
GET /health/ready
```

## Testes

```bash
go test ./...
go test -race ./...
go build ./...
```
