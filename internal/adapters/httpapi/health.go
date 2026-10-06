package httpapi

import (
	"net/http"
	"sync/atomic"
)

type Readiness struct{ ready atomic.Bool }

func NewReadiness() *Readiness { return &Readiness{} }

func (r *Readiness) Set(ready bool) { r.ready.Store(ready) }

func (r *Readiness) Ready() bool { return r.ready.Load() }

func NewHealthHandler(readiness *Readiness) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, _ *http.Request) {
		if !readiness.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}
