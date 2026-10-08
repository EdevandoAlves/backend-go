# s25-server

Serviço Go para processar operações financeiras de wagering. O estado financeiro é persistido no PostgreSQL; HTTP e SQS usam o mesmo caso de uso `ProcessWager`. O projeto ainda não é a entrega completa do desafio.

## Estado atual

Implementado e exercitado:

- Compose com app, PostgreSQL, Keycloak e LocalStack/SQS;
- runtime Uber Fx, migrations no startup e health checks;
- validação OIDC de access token JWT (assinatura/JWKS, issuer, audience e expiração) e claim `provider_id`;
- `POST /wallets` para abertura interna autorizada e `POST /wagering/transactions` para operações de provider;
- idempotência e atomicidade no PostgreSQL;
- consumer da fila FIFO de operações, inbox atômica e publisher da transactional outbox.

Ainda não implementado: endpoints de consulta de transação/saldo/ledger, reconciliação completa de pending rollback e uma cobertura E2E ampla de workers.

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

O Compose expõe a API em `localhost:18080`, Keycloak em `localhost:8081` e LocalStack em `localhost:4566`. As filas são `operations.fifo`, `operations-dlq.fifo` e `events.fifo`.

## Token de provider

O realm importado contém `provider-a`/`provider-b`, secrets de desenvolvimento e audience `backend-go`. O issuer usado pela aplicação é o hostname interno `http://keycloak:8080/realms/backend-go`. Por isso, ao pedir o token no host, use a URL canônica e mapeie somente a conexão para a porta publicada:

```bash
curl --connect-to keycloak:8080:localhost:8081 \
  -X POST 'http://keycloak:8080/realms/backend-go/protocol/openid-connect/token' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode 'grant_type=client_credentials' \
  --data-urlencode 'client_id=provider-a' \
  --data-urlencode 'client_secret=provider-a-dev-secret'
```

Use o `access_token` retornado como `Authorization: Bearer ...`. Não troque `keycloak` por `localhost` na URL do token: isso produziria issuer diferente do configurado no app. Os valores acima são apenas credenciais locais do realm importado.

## Operações HTTP disponíveis

O client interno local usa `internal` / `internal-dev-secret` e precisa da claim `wallet_write`. O token é obtido pelo mesmo endpoint do provider, trocando apenas as credenciais:

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
INTERNAL_TOKEN='cole-o-access_token-interno-aqui'
curl -i http://localhost:18080/wallets \
  -H "Authorization: Bearer $INTERNAL_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"playerId":"player-1","initialBalance":{"amount":"100.00","currency":"BRL"}}'
```

Tokens de provider recebem `403` nessa rota.

### Operação de provider

```bash
TOKEN='cole-o-access_token-local-aqui'
curl -i http://localhost:18080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-bet-001' \
  -d '{"providerId":"provider-a","externalId":"demo-bet-001","playerId":"player-1","walletId":"wallet-1","gameId":"game-1","roundId":"round-1","kind":"BET","money":{"amount":"1.00","currency":"BRL"}}'
```

O body precisa repetir o `provider_id` autenticado. A wallet precisa existir previamente; use a rota interna acima. Não há endpoint de leitura para observar saldo ou ledger.

## Testes

Testes unitários e de integração PostgreSQL:

```bash
go test ./...
go test -race ./...
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/s25_server_integration_test?sslmode=disable' \
  go test -tags=integration ./internal/adapters/postgres ./internal/application
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/s25_server_integration_test?sslmode=disable' \
  go test -race -tags=integration ./internal/adapters/postgres ./internal/application
```

Com o Compose ativo, o teste do consumer SQS também pode ser executado com:

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/s25_server_integration_test?sslmode=disable' \
  go test -tags=integration ./internal/adapters/sqs
```

O fluxo SQS observado é: mensagem válida em `operations.fifo` → persistência de inbox, transação, ledger e outbox em uma unidade PostgreSQL → delete após resultado terminal; o publisher envia o evento para `events.fifo`. Mensagens repetidas/replays não reaplicam o movimento. A cobertura E2E do worker rodando continuamente e de falhas reais de rede ainda é limitada.
