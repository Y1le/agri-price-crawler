package httpx

import (
	"encoding/json"
	"net/http"
)

// Problem is the stable, client-safe error envelope used by the HTTP API.
//
// Detail must contain only text that is safe to show to a client. Internal
// errors belong in server-side logs correlated by TraceID, not in this value.
type Problem struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Status  int    `json:"status"`
	Code    string `json:"code"`
	TraceID string `json:"trace_id"`
	Detail  string `json:"detail,omitempty"`
}

// WriteProblem writes exactly one RFC 9457-style JSON problem document.
func WriteProblem(w http.ResponseWriter, problem Problem) {
	if problem.Status < 100 || problem.Status > 999 {
		problem.Status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)
	_ = json.NewEncoder(w).Encode(problem)
}
