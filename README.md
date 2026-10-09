# backend-go

Serviço Go para processar operações financeiras de wagering. O estado financeiro é persistido no PostgreSQL; HTTP e SQS usam o mesmo caso de uso `ProcessWager`. O projeto ainda não é a entrega completa do desafio.

## Estado atual

Implementado e exercitado:

- Compose com app, PostgreSQL, Keycloak e LocalStack/SQS;
- runtime Uber Fx, migrations no startup e health checks;
- validação OIDC de access token JWT (assinatura/JWKS, issuer, audience e expiração) e claim `provider_id`;
- `POST /wallets` para abertura interna autorizada e `POST /wagering/transactions` para BET, WIN, LOSS, REFUND e ROLLBACK de provider;
- idempotência e atomicidade no PostgreSQL;
- consumer da fila FIFO de operações, inbox atômica e publisher da transactional outbox.

Ainda não implementado: endpoints de consulta de transação/saldo/ledger, persistência de `PENDING_REFERENCE` no fluxo normal quando falta a referência, início de reconciliador como worker e cobertura E2E ampla de workers.

## Subir e parar

Requer Docker Compose e Go instalado para os testes locais.

```bash
docker compose up --build -d
docker compose ps
curl http://localhost:18080/health/live
curl http://localhost:18080/health/ready
docker compose down
```

`docker compose down -v` também remove o volume local do PostgreSQL.

Na primeira inicialização, o Keycloak pode levar algum tempo para subir e importar o realm. Aguarde o serviço ficar saudável antes de solicitar tokens; o app depende dessa prontidão para fazer o discovery OIDC.

O Compose expõe a API em `localhost:18080`, PostgreSQL em `localhost:5433`, Keycloak em `localhost:8081` e LocalStack em `localhost:4566`. As filas são `operations.fifo`, `operations-dlq.fifo` e `events.fifo`.

## Token de provider

O realm importado contém `provider-a`/`provider-b`, secrets de desenvolvimento e audience `backend-go`. O issuer configurado é o hostname canônico interno `http://keycloak:8080/realms/backend-go`; no host, preserve esse hostname e redirecione apenas a conexão para a porta publicada. Não troque `keycloak` por `localhost` na URL: o issuer do token deixaria de corresponder ao configurado. Para guardar o token sem `jq`:

```bash
TOKEN="$(curl --silent --show-error --connect-to keycloak:8080:localhost:8081 \
  -X POST 'http://keycloak:8080/realms/backend-go/protocol/openid-connect/token' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode 'grant_type=client_credentials' \
  --data-urlencode 'client_id=provider-a' \
  --data-urlencode 'client_secret=provider-a-dev-secret' | \
  python3 -c 'import json, sys; print(json.load(sys.stdin)["access_token"])')"
```

## Operações HTTP disponíveis

O client interno local usa `internal` / `internal-dev-secret` e recebe a claim `wallet_write`. Obtenha o token pelo mesmo endpoint canônico:

```bash
curl --connect-to keycloak:8080:localhost:8081 \
  -X POST 'http://keycloak:8080/realms/backend-go/protocol/openid-connect/token' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode 'grant_type=client_credentials' \
  --data-urlencode 'client_id=internal' \
  --data-urlencode 'client_secret=internal-dev-secret'
```

Com esse token, abra a wallet:

```bash
INTERNAL_TOKEN="$(curl --silent --show-error --connect-to keycloak:8080:localhost:8081 \
  -X POST 'http://keycloak:8080/realms/backend-go/protocol/openid-connect/token' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode 'grant_type=client_credentials' \
  --data-urlencode 'client_id=internal' \
  --data-urlencode 'client_secret=internal-dev-secret' | \
  python3 -c 'import json, sys; print(json.load(sys.stdin)["access_token"])')"
WALLET_RESPONSE="$(curl --silent --show-error http://localhost:18080/wallets \
  -H "Authorization: Bearer $INTERNAL_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"playerId":"player-1","initialBalance":{"amount":"100.00","currency":"BRL"}}')"
WALLET_ID="$(python3 -c 'import json, sys; print(json.load(sys.stdin)["walletId"])' <<<"$WALLET_RESPONSE")"
```

Tokens de provider recebem `403` nessa rota.

### Operação de provider

```bash
curl -i http://localhost:18080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-bet-001' \
  -d "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"demo-bet-001\",\"playerId\":\"player-1\",\"walletId\":\"$WALLET_ID\",\"gameId\":\"game-1\",\"roundId\":\"round-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"1.00\",\"currency\":\"BRL\"}}"
```

O body precisa repetir o `provider_id` autenticado. A wallet precisa existir previamente; use a rota interna acima. Não há endpoint de leitura para observar saldo ou ledger.

### Operação SQS

Com o Compose ativo e `WALLET_ID` definido pelo exemplo acima, liste as filas e envie um BET FIFO com IDs diferentes do exemplo HTTP:

```bash
docker compose exec -T localstack awslocal sqs list-queues

SQS_MESSAGE_ID='sqs-demo-20261008-001'
SQS_EXTERNAL_ID='sqs-demo-external-20261008-001'
SQS_IDEMPOTENCY_KEY='sqs-demo-idem-20261008-001'
docker compose exec -T localstack awslocal sqs send-message \
  --queue-url 'http://localhost:4566/000000000000/operations.fifo' \
  --message-group-id 'provider-a-sqs-demo-20261008' \
  --message-deduplication-id 'sqs-demo-dedup-20261008-001' \
  --message-body "{\"messageId\":\"$SQS_MESSAGE_ID\",\"version\":1,\"type\":\"WagerTransactionRequested\",\"occurredAt\":\"2026-10-08T12:00:00Z\",\"data\":{\"providerId\":\"provider-a\",\"externalTransactionId\":\"$SQS_EXTERNAL_ID\",\"idempotencyKey\":\"$SQS_IDEMPOTENCY_KEY\",\"playerId\":\"player-1\",\"walletId\":\"$WALLET_ID\",\"gameId\":\"game-sqs-1\",\"roundId\":\"round-sqs-20261008-001\",\"kind\":\"BET\",\"money\":{\"amount\":\"2.00\",\"currency\":\"BRL\"},\"referenceExternalTransactionId\":null}}"
```

O consumer processa a mensagem e o publisher envia o evento para `events.fifo`. Receba os eventos disponíveis (a fila pode conter eventos de execuções anteriores):

```bash
docker compose exec -T localstack awslocal sqs receive-message \
  --queue-url 'http://localhost:4566/000000000000/events.fifo' \
  --max-number-of-messages 10 \
  --wait-time-seconds 10
```

## Testes

Testes unitários e de integração PostgreSQL:

```bash
go test ./...
go test -race ./...
docker compose exec -T postgres createdb -U postgres s25_server_integration_test || true
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5433/s25_server_integration_test?sslmode=disable' \
  go test -count=1 -tags=integration ./internal/adapters/postgres
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5433/s25_server_integration_test?sslmode=disable' \
  go test -count=1 -tags=integration ./internal/application
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5433/s25_server_integration_test?sslmode=disable' \
  go test -count=1 -tags=integration ./internal/adapters/sqs
```

Execute os pacotes de integração separadamente: todos recriam o schema `public` do mesmo banco de teste para partir de estado conhecido.

Para repetir as mesmas provas com race detector:

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5433/s25_server_integration_test?sslmode=disable' \
  go test -count=1 -race -tags=integration ./internal/adapters/postgres
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5433/s25_server_integration_test?sslmode=disable' \
  go test -count=1 -race -tags=integration ./internal/application
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5433/s25_server_integration_test?sslmode=disable' \
  go test -count=1 -race -tags=integration ./internal/adapters/sqs
```

Com o Compose ativo, o teste do consumer SQS também pode ser executado com:

```bash
docker compose exec -T postgres createdb -U postgres s25_server_integration_test || true
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5433/s25_server_integration_test?sslmode=disable' \
  go test -count=1 -tags=integration ./internal/adapters/sqs
```

O fluxo SQS observado é: mensagem válida em `operations.fifo` → persistência de inbox, transação, ledger e outbox em uma unidade PostgreSQL → delete após resultado terminal; o publisher envia o evento para `events.fifo`. Mensagens repetidas/replays não reaplicam o movimento. A cobertura E2E do worker rodando continuamente e de falhas reais de rede ainda é limitada.
