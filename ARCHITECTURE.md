# Arquitetura implementada

Este documento descreve o estado atual, não a arquitetura-alvo inteira do desafio.

## Runtime e HTTP

`cmd/server` monta a aplicação com Uber Fx: configuração, `slog`, PostgreSQL, migrations, OIDC, HTTP, SQS e workers. O Compose executa app, PostgreSQL, Keycloak e LocalStack. Fx inicia o servidor, consumer e publisher com contexto cancelável e aguarda as goroutines no shutdown.

As rotas são `GET /health/live`, `GET /health/ready`, `POST /wallets` e `POST /wagering/transactions`. Ainda faltam consultas de transação, saldo e ledger.

## Dinheiro e processamento financeiro

`Money` guarda `int64` em centavos e moeda; o JSON usa decimal string com duas casas. HTTP e SQS convergem para `application.ProcessWagerService`.

Cada efeito financeiro usa uma transação SQL explícita: deduplicação por provider/chave e hash, lock `SELECT ... FOR UPDATE` somente na wallet, validação/alteração da wallet, ledger append-only, transação externa e outbox no mesmo commit. PostgreSQL protege saldo não negativo, unicidades, matemática e imutabilidade do ledger. A idempotência persiste por `(provider_id, idempotency_key)` e `(provider_id, external_id)`.

O HTTP aceita BET, WIN, LOSS, REFUND e ROLLBACK com `externalTransactionId` e referência quando aplicável. As regras financeiras permanecem no serviço.

## OIDC e Keycloak

O app faz discovery no startup e valida JWT por JWKS, issuer, audience `backend-go`, expiração e identidade `provider_id`. A rota de provider rejeita `providerId` diferente do claim. A rota interna exige identidade interna e `wallet:write`. O realm local fornece os clients de desenvolvimento e claims necessários.

## Inbox, outbox e SQS

LocalStack cria `operations.fifo`, `operations-dlq.fifo` e `events.fifo`. O consumer valida o envelope v1, grava `inbox_messages` na mesma transação de `ProcessWager` e só apaga a mensagem depois de resultado persistido ou replay terminal. A outbox é criada com o efeito financeiro; o publisher reivindica registros, faz o I/O SQS fora da transação e marca publicação ou tentativa posterior. `eventId` estável oferece entrega at-least-once, não exactly-once.

## Limitações honestas

- o fluxo normal de referência ausente ainda retorna indisponibilidade: não persiste `PENDING_REFERENCE` e não inicia o reconciliador;
- existe um serviço reconciliador testado para REFUND pendente inserido no banco, mas ele não é worker operacional; ROLLBACK pendente não deve ser alegado como completo;
- faltam endpoints de consulta;
- falta cobertura E2E completa de workers contínuos, retry/DLQ, shutdown e convergência HTTP + SQS;
- não há garantia exactly-once entre PostgreSQL e SQS.
