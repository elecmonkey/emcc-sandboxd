package src

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// randomID generates a random hex string of given length
func randomID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HandleCompile handles the compilation request
func (s *Server) HandleCompile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.ensureDirs(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var req CompileRequest
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
	args := s.MergeAndFilterArgs(req.Args)
	// Always force output naming & paths
	args = append(args, "-o", "app.js")

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
		resp := CompileResponse{OK: false, ID: id, Error: string(out)}
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
	resp := CompileResponse{
		OK:   true,
		ID:   id,
		JS:   fmt.Sprintf("%s/%s/app.js", baseURL, id),
		WASM: fmt.Sprintf("%s/%s/app.wasm", baseURL, id),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}