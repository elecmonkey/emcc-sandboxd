package main

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "sync"
    "time"
)

type Config struct {
    Addr                 string        `json:"addr"`
    BaseDir              string        `json:"baseDir"`
    JobsDir              string        `json:"jobsDir"`
    ArtifactsDir         string        `json:"artifactsDir"`
    EnableStaticArtifacts bool         `json:"enableStaticArtifacts"`
    ArtifactTTL          time.Duration `json:"-"`
    ArtifactTTLDays      int           `json:"artifactTTLDays"`
    CleanupIntervalMins  int           `json:"cleanupIntervalMins"`
    DefaultArgs          []string      `json:"defaultArgs"`
    NsJailEnabled        bool          `json:"nsjailEnabled"`
    NsJailPath           string        `json:"nsjailPath"`
    CgroupV2Root         string        `json:"cgroupV2Root"`
    EnableResourceGating bool          `json:"enableResourceGating"`
    JobMemoryEstimateMB  int64         `json:"jobMemoryEstimateMB"`
}

func defaultConfig() Config {
    return Config{
        Addr:                  ":8080",
        BaseDir:               ".",
        JobsDir:               "jobs",
        ArtifactsDir:          "artifacts",
        EnableStaticArtifacts: true,
        ArtifactTTLDays:       3,
        CleanupIntervalMins:   30,
        DefaultArgs: []string{
            "-sINVOKE_RUN=0",
            "-sENVIRONMENT=web",
            "-sALLOW_MEMORY_GROWTH=1",
            "-sMODULARIZE=1",
        },
        NsJailEnabled: false,
        NsJailPath:    "nsjail",
        CgroupV2Root:  "cgroup",
        EnableResourceGating: false,
        JobMemoryEstimateMB:  256,
    }
}

func loadConfig(path string) (Config, error) {
    cfg := defaultConfig()
    f, err := os.Open(path)
    if err != nil {
        // No config file is fine; return defaults
        return cfg, nil
    }
    defer f.Close()
    dec := json.NewDecoder(f)
    if err := dec.Decode(&cfg); err != nil {
        return cfg, err
    }
    // derive durations
    cfg.ArtifactTTL = time.Duration(cfg.ArtifactTTLDays) * 24 * time.Hour
    return cfg, nil
}

type compileRequest struct {
    Code string   `json:"code"`
    Type string   `json:"type"` // "c" or "cpp"
    Args []string `json:"args"`
}

type compileResponse struct {
    OK    bool   `json:"ok"`
    ID    string `json:"id"`
    JS    string `json:"js"`
    WASM  string `json:"wasm"`
    Error string `json:"error,omitempty"`
}

type Server struct {
    cfg       Config
    httpSrv   *http.Server
    onceMkDir sync.Once
    // resource gating state
    mu               sync.Mutex
    memBudgetBytes   int64
    memReservedBytes int64
}

func NewServer(cfg Config) *Server {
    if cfg.ArtifactTTL == 0 {
        cfg.ArtifactTTL = time.Duration(cfg.ArtifactTTLDays) * 24 * time.Hour
    }
    s := &Server{cfg: cfg}
    return s
}

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

func randomID(n int) (string, error) {
    b := make([]byte, n)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return hex.EncodeToString(b), nil
}

func (s *Server) handleCompile(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    if err := s.ensureDirs(); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    var req compileRequest
    dec := json.NewDecoder(r.Body)
    if err := dec.Decode(&req); err != nil {
        http.Error(w, "invalid JSON", http.StatusBadRequest)
        return
    }
    if strings.TrimSpace(req.Code) == "" {
        http.Error(w, "code is required", http.StatusBadRequest)
        return
    }
    lang := strings.ToLower(strings.TrimSpace(req.Type))
    if lang != "c" && lang != "cpp" && lang != "cc" && lang != "c++" && lang != "" {
        http.Error(w, "type must be 'c' or 'cpp'", http.StatusBadRequest)
        return
    }
    if lang == "" {
        // default to c
        lang = "c"
    }

    // Resource gating by cgroup memory budget if enabled
    ctx := r.Context()
    if s.cfg.EnableResourceGating {
        if err := s.ensureMemBudget(); err != nil {
            http.Error(w, "resource gating init failed: "+err.Error(), http.StatusInternalServerError)
            return
        }
        est := s.cfg.JobMemoryEstimateMB * 1024 * 1024
        if est <= 0 {
            est = 256 * 1024 * 1024
        }
        if err := s.acquireMemory(ctx, est); err != nil {
            http.Error(w, "resource wait canceled", http.StatusRequestTimeout)
            return
        }
        defer s.releaseMemory(est)
    }

    id, _ := randomID(4) // 8 hex chars
    jobDir := filepath.Join(s.cfg.BaseDir, s.cfg.JobsDir, id)
    artDir := filepath.Join(s.cfg.BaseDir, s.cfg.ArtifactsDir, id)
    if err := os.MkdirAll(jobDir, 0o755); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    if err := os.MkdirAll(artDir, 0o755); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    srcName := "main.c"
    if lang != "c" {
        srcName = "main.cpp"
    }
    srcPath := filepath.Join(jobDir, srcName)
    if err := os.WriteFile(srcPath, []byte(req.Code), 0o644); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Build argument list
    args := s.mergeAndFilterArgs(req.Args)
    // Always force output naming & paths
    outJS := filepath.Join(jobDir, "app.js")
    args = append(args, "-o", outJS)

    // Choose compiler
    compiler := "emcc"
    if lang != "c" {
        compiler = "em++"
    }

    // Execute compile
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    var cmd *exec.Cmd
    if s.cfg.NsJailEnabled {
        // Run within nsjail if enabled. We bind mount jobDir to /work and compile there.
        nsArgs := []string{
            "--quiet",
            "--iface_no_lo",
            "--cwd", "/work",
            "--bindmount", fmt.Sprintf("%s:/work", jobDir),
            "--rlimit_fsize", fmt.Sprintf("%d", 256*1024*1024), // 256MiB
            "--",
            compiler,
            srcName,
        }
        nsArgs = append(nsArgs, args...)
        cmd = exec.CommandContext(ctx, s.cfg.NsJailPath, nsArgs...)
    } else {
        // Direct execution fallback (for local dev / MVP)
        fullArgs := append([]string{srcName}, args...)
        cmd = exec.CommandContext(ctx, compiler, fullArgs...)
        cmd.Dir = jobDir
    }

    // Inherit minimal environment for emscripten if needed
    cmd.Env = os.Environ()

    out, err := cmd.CombinedOutput()
    if err != nil {
        // Return compile error details
        resp := compileResponse{OK: false, ID: id, Error: string(out)}
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadRequest)
        _ = json.NewEncoder(w).Encode(resp)
        return
    }

    // Move artifacts to artifacts/<id>
    // Emscripten will place .wasm next to .js
    wasmSrc := filepath.Join(jobDir, "app.wasm")
    jsSrc := filepath.Join(jobDir, "app.js")
    jsDst := filepath.Join(artDir, "app.js")
    wasmDst := filepath.Join(artDir, "app.wasm")
    // Best-effort copy/move
    _ = os.Rename(jsSrc, jsDst)
    _ = os.Rename(wasmSrc, wasmDst)

    // Cleanup job dir (best-effort)
    _ = os.RemoveAll(jobDir)

    // Respond with URLs
    baseURL := "/" + strings.TrimPrefix(s.cfg.ArtifactsDir, "/")
    resp := compileResponse{
        OK:   true,
        ID:   id,
        JS:   fmt.Sprintf("%s/%s/app.js", baseURL, id),
        WASM: fmt.Sprintf("%s/%s/app.wasm", baseURL, id),
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(resp)
}

// ensureMemBudget initializes memBudgetBytes by reading cgroup v2 memory.max if available.
func (s *Server) ensureMemBudget() error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.memBudgetBytes > 0 {
        return nil
    }
    max, err := readCgroupMemoryMax(s.cfg.CgroupV2Root)
    if err != nil {
        return err
    }
    if max <= 0 {
        // Unlimited; disable gating effectively
        s.memBudgetBytes = 0
        return nil
    }
    s.memBudgetBytes = max
    return nil
}

func (s *Server) acquireMemory(ctx context.Context, estimateBytes int64) error {
    ticker := time.NewTicker(200 * time.Millisecond)
    defer ticker.Stop()
    for {
        // fast path: try lock + check
        s.mu.Lock()
        max := s.memBudgetBytes
        s.mu.Unlock()

        if max == 0 { // unlimited or not configured
            s.mu.Lock()
            s.memReservedBytes += estimateBytes
            s.mu.Unlock()
            return nil
        }

        cur, err := readCgroupMemoryCurrent(s.cfg.CgroupV2Root)
        if err != nil {
            // if cannot read, be safe and wait a bit
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-ticker.C:
                continue
            }
        }

        // Attempt reservation atomically
        s.mu.Lock()
        if cur+s.memReservedBytes+estimateBytes <= max {
            s.memReservedBytes += estimateBytes
            s.mu.Unlock()
            return nil
        }
        s.mu.Unlock()

        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
        }
    }
}

func (s *Server) releaseMemory(estimateBytes int64) {
    s.mu.Lock()
    if estimateBytes > s.memReservedBytes {
        s.memReservedBytes = 0
    } else {
        s.memReservedBytes -= estimateBytes
    }
    s.mu.Unlock()
}

func readCgroupMemoryMax(root string) (int64, error) {
    if root == "" {
        return 0, fmt.Errorf("cgroup root not set")
    }
    b, err := os.ReadFile(filepath.Join(root, "memory.max"))
    if err != nil {
        return 0, err
    }
    s := strings.TrimSpace(string(b))
    if s == "max" {
        return 0, nil
    }
    var v int64
    _, err = fmt.Sscanf(s, "%d", &v)
    if err != nil {
        return 0, err
    }
    return v, nil
}

func readCgroupMemoryCurrent(root string) (int64, error) {
    b, err := os.ReadFile(filepath.Join(root, "memory.current"))
    if err != nil {
        return 0, err
    }
    var v int64
    _, err = fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &v)
    if err != nil {
        return 0, err
    }
    return v, nil
}

// mergeAndFilterArgs merges default args with user args, filtering by whitelist
func (s *Server) mergeAndFilterArgs(user []string) []string {
    // Start with defaults (already safe)
    result := append([]string{}, s.cfg.DefaultArgs...)

    // Allowlist patterns
    allowedPrefix := []string{
        "-O0", "-O1", "-O2", "-O3", "-Os", "-Oz",
        "-g", "-g4",
        "-sMODULARIZE=",
        "-sENVIRONMENT=",
        "-sINVOKE_RUN=",
        "-sEXPORTED_FUNCTIONS=",
        "-sEXPORTED_RUNTIME_METHODS=",
        "-sALLOW_MEMORY_GROWTH=",
        "--preload-file",
        "--embed-file",
        "--source-map-base",
    }
    // Disallowed exact/prefixes
    blocked := []string{
        "-o",
        "--shell-file",
        "-sFORCE_FILESYSTEM",
        "-sENVIRONMENT=node",
    }

    // Normalize and filter
    for i := 0; i < len(user); i++ {
        a := strings.TrimSpace(user[i])
        if a == "" {
            continue
        }
        // Disallow path escapes in multi-part flags like --preload-file path
        if (a == "--preload-file" || a == "--embed-file" || a == "--source-map-base") && i+1 < len(user) {
            next := strings.TrimSpace(user[i+1])
            if !safeArgPath(next) {
                i++ // skip paired next
                continue
            }
            result = append(result, a, next)
            i++ // consumed next
            continue
        }

        if isBlockedArg(a, blocked) {
            continue
        }
        if isAllowedArg(a, allowedPrefix) {
            result = append(result, a)
        }
    }
    return result
}

func isBlockedArg(a string, blocked []string) bool {
    for _, b := range blocked {
        if a == b || strings.HasPrefix(a, b+"=") {
            return true
        }
    }
    return false
}

func isAllowedArg(a string, allowed []string) bool {
    // exact match or prefix match with '=' are both considered via prefix list
    for _, p := range allowed {
        if strings.HasPrefix(a, p) || a == p {
            return true
        }
    }
    return false
}

func safeArgPath(p string) bool {
    // Deny absolute paths and parent escapes
    if strings.HasPrefix(p, "/") {
        return false
    }
    if strings.Contains(p, "..") {
        return false
    }
    return true
}

func (s *Server) startCleanupLoop() {
    dir := filepath.Join(s.cfg.BaseDir, s.cfg.ArtifactsDir)
    if s.cfg.ArtifactTTL <= 0 {
        s.cfg.ArtifactTTL = time.Duration(s.cfg.ArtifactTTLDays) * 24 * time.Hour
    }
    interval := time.Duration(s.cfg.CleanupIntervalMins) * time.Minute
    if interval <= 0 {
        interval = 30 * time.Minute
    }
    ttl := s.cfg.ArtifactTTL
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for {
            entries, err := os.ReadDir(dir)
            if err == nil {
                for _, e := range entries {
                    fi, err := os.Stat(filepath.Join(dir, e.Name()))
                    if err != nil || !fi.IsDir() {
                        continue
                    }
                    if time.Since(fi.ModTime()) > ttl {
                        _ = os.RemoveAll(filepath.Join(dir, e.Name()))
                    }
                }
            }
            <-ticker.C
        }
    }()
}

func (s *Server) routes(mux *http.ServeMux) {
    mux.HandleFunc("/compile", s.handleCompile)
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok")) })
    if s.cfg.EnableStaticArtifacts {
        fs := http.StripPrefix("/"+strings.TrimPrefix(s.cfg.ArtifactsDir, "/"), http.FileServer(http.Dir(filepath.Join(s.cfg.BaseDir, s.cfg.ArtifactsDir))))
        mux.Handle("/"+strings.TrimPrefix(s.cfg.ArtifactsDir, "/")+"/", fs)
    }
}

func (s *Server) Start(ctx context.Context) error {
    if err := s.ensureDirs(); err != nil {
        return err
    }
    s.startCleanupLoop()
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

func logRequest(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        log.Printf("%s %s", r.Method, r.URL.Path)
        next.ServeHTTP(w, r)
    })
}

func main() {
    // Optional config.json in working directory
    cfg, err := loadConfig("config.json")
    if err != nil {
        log.Fatalf("load config: %v", err)
    }
    // Validate minimal external deps when nsjail enabled
    if cfg.NsJailEnabled {
        if _, err := exec.LookPath(cfg.NsJailPath); err != nil {
            log.Fatalf("nsjail enabled but not found at '%s'", cfg.NsJailPath)
        }
    }
    if err := validateDirs(cfg); err != nil {
        log.Fatalf("invalid dirs: %v", err)
    }
    srv := NewServer(cfg)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
        log.Fatalf("server: %v", err)
    }
}

func validateDirs(cfg Config) error {
    if cfg.BaseDir == "" {
        return fmt.Errorf("baseDir empty")
    }
    // Ensure base dir exists
    if err := os.MkdirAll(cfg.BaseDir, 0o755); err != nil {
        return err
    }
    return nil
}
