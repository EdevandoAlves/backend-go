package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthLiveAlwaysReturnsOK(t *testing.T) {
	server := httptest.NewServer(NewHealthHandler(NewReadiness()))
	defer server.Close()

	response, err := http.Get(server.URL + "/health/live")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestHealthReadyCanBeControlled(t *testing.T) {
	readiness := NewReadiness()
	server := httptest.NewServer(NewHealthHandler(readiness))
	defer server.Close()

	assertStatus(t, server.URL+"/health/ready", http.StatusServiceUnavailable)
	readiness.Set(true)
	assertStatus(t, server.URL+"/health/ready", http.StatusOK)
	readiness.Set(false)
	assertStatus(t, server.URL+"/health/ready", http.StatusServiceUnavailable)
}

func assertStatus(t *testing.T, url string, want int) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("status = %d, want %d", response.StatusCode, want)
	}
}
