package src

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Server represents the HTTP server and its configuration
type Server struct {
	cfg       Config
	httpSrv   *http.Server
	onceMkDir sync.Once
	// resource gating state
	mu               sync.Mutex
	memBudgetBytes   int64
	memReservedBytes int64
}

// NewServer creates a new server instance with the given configuration
func NewServer(cfg Config) *Server {
	if cfg.ArtifactTTL == 0 {
		cfg.ArtifactTTL = time.Duration(cfg.ArtifactTTLDays) * 24 * time.Hour
	}
	s := &Server{cfg: cfg}
	return s
}

// ensureDirs ensures that required directories exist
func (s *Server) ensureDirs() error {
	var err error
	s.onceMkDir.Do(func() {
		err = os.MkdirAll(filepath.Join(s.cfg.BaseDir, s.cfg.JobsDir), 0o755)
		if err != nil {
			return
		}
		err = os.MkdirAll(filepath.Join(s.cfg.BaseDir, s.cfg.ArtifactsDir), 0o755)
		if err != nil {
			return
		}
		// cgroup path optional; do not create by default
	})
	return err
}

// routes sets up the HTTP routes
func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/compile", s.HandleCompile)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { 
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok")) 
	})
	if s.cfg.EnableStaticArtifacts {
		fs := http.StripPrefix("/"+strings.TrimPrefix(s.cfg.ArtifactsDir, "/"), 
			http.FileServer(http.Dir(filepath.Join(s.cfg.BaseDir, s.cfg.ArtifactsDir))))
		mux.Handle("/"+strings.TrimPrefix(s.cfg.ArtifactsDir, "/")+"/", fs)
	}
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	if err := s.ensureDirs(); err != nil {
		return err
	}
	s.StartCleanupLoop()
	mux := http.NewServeMux()
	s.routes(mux)
	s.httpSrv = &http.Server{Addr: s.cfg.Addr, Handler: logRequest(mux)}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(c)
	}()
	log.Printf("emcc-sandboxd listening on %s", s.cfg.Addr)
	return s.httpSrv.ListenAndServe()
}

// logRequest is a middleware that logs HTTP requests
func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}