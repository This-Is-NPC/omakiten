package syncserver

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"omakiten/internal/domain"
	"omakiten/internal/syncproto"
)

// maxBodyBytes caps a push body.
const maxBodyBytes = 16 << 20

// Error codes the handler raises outside the domain catalog.
const (
	codeUnauthorized     = "unauthorized"
	codeInvalidBody      = "invalid_body"
	codeBodyTooLarge     = "body_too_large"
	codeInvalidParameter = "invalid_parameter"
	codeInternal         = "internal_error"
)

// errorBody is the JSON error envelope, shaped like the local HTTP API's.
type errorBody struct {
	OK      bool           `json:"ok"`
	Code    string         `json:"code"`
	Message string         `json:"msg"`
	Details map[string]any `json:"details,omitempty"`
}

type handler struct {
	store  *Store
	tokens [][]byte
}

// NewHandler serves the sync routes over store for clients bearing any of tokens.
func NewHandler(store *Store, tokens []string) http.Handler {
	h := &handler{store: store}
	for _, t := range tokens {
		h.tokens = append(h.tokens, []byte(t))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+syncproto.ProjectsPath, h.listProjects)
	mux.HandleFunc("POST "+syncproto.ProjectsPath+"/{uid}/changes", h.push)
	mux.HandleFunc("GET "+syncproto.ProjectsPath+"/{uid}/changes", h.pull)
	return h.authorize(mux)
}

func (h *handler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !h.knows([]byte(token)) {
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "missing or invalid bearer token", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// knows compares against every token so timing does not reveal which matched.
func (h *handler) knows(token []byte) bool {
	match := 0
	for _, t := range h.tokens {
		match |= subtle.ConstantTimeCompare(t, token)
	}
	return match == 1
}

func (h *handler) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.store.Projects(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, syncproto.ProjectsResponse{Projects: projects})
}

func (h *handler) push(w http.ResponseWriter, r *http.Request) {
	uid, ok := pathUID(w, r)
	if !ok {
		return
	}
	var req syncproto.PushRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeBodyError(w, err)
		return
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		writeBodyError(w, err)
		return
	}
	if req.Project.UID != uid {
		writeError(w, http.StatusBadRequest, string(domain.ErrValidation), "project uid does not match the path",
			map[string]any{"path": string(uid), "body": string(req.Project.UID)})
		return
	}
	resp, err := h.store.Push(r.Context(), req, time.Now())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, resp)
}

func (h *handler) pull(w http.ResponseWriter, r *http.Request) {
	uid, ok := pathUID(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	after, ok := intParam(w, query.Get(syncproto.QueryAfter), syncproto.QueryAfter)
	if !ok {
		return
	}
	limit, ok := intParam(w, query.Get(syncproto.QueryLimit), syncproto.QueryLimit)
	if !ok {
		return
	}
	resp, err := h.store.Pull(r.Context(), uid, after, int(limit))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, resp)
}

// writeBodyError answers a push body that failed to read: 413 when it exceeded the cap, 400 otherwise.
func writeBodyError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, codeBodyTooLarge, "request body exceeds the size limit",
			map[string]any{"limit": tooLarge.Limit})
	case err == nil:
		writeError(w, http.StatusBadRequest, codeInvalidBody, "body must contain one JSON value", nil)
	default:
		writeError(w, http.StatusBadRequest, codeInvalidBody, "invalid request body", map[string]any{"error": err.Error()})
	}
}

func pathUID(w http.ResponseWriter, r *http.Request) (domain.UID, bool) {
	uid, err := domain.ParseUID(r.PathValue("uid"))
	if err != nil {
		fail(w, err)
		return "", false
	}
	return uid, true
}

// intParam parses an optional non-negative integer query value; empty is zero.
func intParam(w http.ResponseWriter, raw, name string) (int64, bool) {
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "query parameter "+name+" must be a non-negative integer",
			map[string]any{"parameter": name, "value": raw})
		return 0, false
	}
	return n, true
}

func fail(w http.ResponseWriter, err error) {
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		slog.Error("sync request failed", "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error", nil)
		return
	}
	status := http.StatusBadRequest
	if coded.Code == domain.ErrProjectNotFound {
		status = http.StatusNotFound
	}
	writeError(w, status, string(coded.Code), coded.Message, coded.Details)
}

func writeError(w http.ResponseWriter, status int, code, msg string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Code: code, Message: msg, Details: details})
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
