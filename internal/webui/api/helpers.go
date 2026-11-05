package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// writeJSON writes v as a JSON response with the given status. An encode
// error can only be logged: the status line has already been sent.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("Error encoding %d response: %v", status, err)
	}
}

// errorResponse is the body of every API error.
type errorResponse struct {
	Error string `json:"error"`
}

// writeError writes {"error": msg} as JSON with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// pathID parses the {id} path value. On failure it writes the 400 itself
// and returns false.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid event ID")
		return 0, false
	}
	return id, true
}
