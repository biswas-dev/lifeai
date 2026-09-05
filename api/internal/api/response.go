package api

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

var logger = zap.NewNop()

// SetLogger installs the package logger used by the response helpers.
func SetLogger(l *zap.Logger) {
	if l != nil {
		logger = l
	}
}

type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.Error("failed to encode response", zap.Error(err))
	}
}

func respondError(w http.ResponseWriter, status int, message string, code ...string) {
	body := errorBody{Error: message}
	if len(code) > 0 {
		body.Code = code[0]
	}
	respondJSON(w, status, body)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalid_body")
		return false
	}
	return true
}

// readAll reads a reader fully with a hard cap.
func readAll(r io.Reader) ([]byte, error) {
	const maxBytes = 20 << 20
	return io.ReadAll(io.LimitReader(r, maxBytes))
}

// scanner is what *sql.Row and *sql.Rows have in common.
type scanner interface {
	Scan(dest ...any) error
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed")
}

func nullFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func strPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
