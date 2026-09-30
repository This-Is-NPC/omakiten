package httpapi

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"time"

	"omakiten/internal/output"
)

// Options wires the adapter to its ports.
type Options struct {
	// Token authenticates every route except /health as a Bearer credential.
	Token    string
	Version  string
	Runtimes Runtimes
	Hub      *Hub
	Log      EventLog
	// Heartbeat is the SSE keep-alive interval.
	Heartbeat time.Duration
}

// Server is the HTTP handler: loopback Host check, Bearer authentication,
// then the route table.
type Server struct {
	opts   Options
	mux    *http.ServeMux
	public map[string]bool
	spec   []byte
}

func New(opts Options) (*Server, error) {
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 15 * time.Second
	}
	s := &Server{opts: opts, mux: http.NewServeMux(), public: map[string]bool{}}
	routes := s.routes()
	spec, err := buildSpec(routes)
	if err != nil {
		return nil, err
	}
	s.spec = spec
	for _, rt := range routes {
		if rt.public {
			s.public[rt.path] = true
		}
		s.mux.Handle(rt.method+" "+rt.path, s.handler(rt))
	}
	s.mux.HandleFunc("GET /api/v1/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.spec)
	})
	s.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		s.writeError(w, &adapterError{status: http.StatusNotFound, code: codeRouteNotFound})
	})
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !loopbackHost(r.Host) {
		s.writeError(w, &adapterError{status: http.StatusForbidden, code: codeHostRejected, details: map[string]any{"host": r.Host}})
		return
	}
	if !s.public[r.URL.Path] && !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		s.writeError(w, &adapterError{status: http.StatusUnauthorized, code: codeUnauthorized})
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handler(rt route) http.Handler {
	if rt.stream != nil {
		return rt.stream
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := rt.serve(w, r)
		if err != nil {
			s.writeError(w, err)
			return
		}
		writeEnvelope(w, http.StatusOK, output.Success(data))
	})
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	status, envelope := failure(err, s.opts.Runtimes.Catalog())
	writeEnvelope(w, status, envelope)
}

func (s *Server) authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && s.opts.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.opts.Token)) == 1
}

// loopbackHost rejects DNS-rebinding requests whose Host names anything
// but the loopback interface.
func loopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
