package httpx_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Y1le/agri-price-crawler/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

type orderedResponseWriter struct {
	header          http.Header
	status          int
	contentTypeSeen string
	writes          int
	body            []byte
}

func (w *orderedResponseWriter) Header() http.Header {
	return w.header
}

func (w *orderedResponseWriter) WriteHeader(status int) {
	w.status = status
	w.contentTypeSeen = w.header.Get("Content-Type")
}

func (w *orderedResponseWriter) Write(body []byte) (int, error) {
	w.writes++
	w.body = append(w.body, body...)
	return len(body), nil
}

func TestWriteProblemWritesOneProblemJSONDocument(t *testing.T) {
	t.Parallel()

	writer := &orderedResponseWriter{header: make(http.Header)}
	want := httpx.Problem{
		Type:    "https://api.example.test/problems/invalid-request",
		Title:   "请求无效",
		Status:  http.StatusBadRequest,
		Code:    "invalid_request",
		TraceID: "request-123",
		Detail:  "请检查请求参数。",
	}

	httpx.WriteProblem(writer, want)

	require.Equal(t, http.StatusBadRequest, writer.status)
	require.Equal(t, "application/problem+json", writer.contentTypeSeen)
	require.Equal(t, 1, writer.writes)

	var got httpx.Problem
	require.NoError(t, json.Unmarshal(writer.body, &got))
	require.Equal(t, want, got)

	var extra any
	decoder := json.NewDecoder(bytes.NewReader(writer.body))
	require.NoError(t, decoder.Decode(&extra))
	require.Error(t, decoder.Decode(&extra), "the response must contain exactly one JSON document")
	require.NotContains(t, string(writer.body), "database connection refused")
}

func TestWriteProblemOmitsEmptySafeDetail(t *testing.T) {
	t.Parallel()

	writer := &orderedResponseWriter{header: make(http.Header)}
	httpx.WriteProblem(writer, httpx.Problem{
		Type:    "about:blank",
		Title:   "内部错误",
		Status:  http.StatusInternalServerError,
		Code:    "internal_error",
		TraceID: "request-456",
	})

	require.NotContains(t, string(writer.body), `"detail"`)
}
