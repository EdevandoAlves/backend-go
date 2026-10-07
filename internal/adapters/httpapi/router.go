package httpapi

import (
	"net/http"
	"time"
)

func NewTransactionHandler(identity ProviderIdentity, executor WagerExecutor, now func() time.Time, ids IDFunc) http.Handler {
	return TransactionHandler{Identity: identity, Executor: executor, Now: now, ID: ids}
}
