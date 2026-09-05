package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestAuthenticatedRouteDeadlines(t *testing.T) {
	for _, tc := range []struct {
		path    string
		minimum time.Duration
		maximum time.Duration
	}{
		{"/api/import/apple-health", 14 * time.Minute, 15 * time.Minute},
		{"/api/import/samsung-health", 14 * time.Minute, 15 * time.Minute},
		{"/api/ai/plan", 3 * time.Minute, 4 * time.Minute},
		{"/api/blood/reports/upload", 3 * time.Minute, 4 * time.Minute},
		{"/api/me", 55 * time.Second, time.Minute},
	} {
		t.Run(tc.path, func(t *testing.T) {
			router := chi.NewRouter()
			router.Route("/api", func(r chi.Router) {
				r.Group(func(r chi.Router) {
					r.Use(authenticatedTimeout)
					r.HandleFunc("/*", func(w http.ResponseWriter, req *http.Request) {
						deadline, ok := req.Context().Deadline()
						remaining := time.Until(deadline)
						if !ok || remaining < tc.minimum || remaining > tc.maximum {
							t.Errorf("unexpected request deadline: %v (present=%v)", remaining, ok)
						}
						w.WriteHeader(http.StatusNoContent)
					})
				})
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tc.path, nil))
			if response.Code != http.StatusNoContent {
				t.Fatalf("route not reached: %d", response.Code)
			}
		})
	}
}
