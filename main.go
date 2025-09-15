package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"

	"emcc-sandboxd/src"
)

func main() {
	// Optional config.json in working directory
	cfg, err := src.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Change working directory if specified
	if cfg.WorkingDir != "" {
		if err := os.Chdir(cfg.WorkingDir); err != nil {
			log.Fatalf("failed to change working directory to '%s': %v", cfg.WorkingDir, err)
		}
		log.Printf("Changed working directory to: %s", cfg.WorkingDir)
	}

	// Validate minimal external deps when nsjail enabled
	if cfg.NsJailEnabled {
		if _, err := exec.LookPath(cfg.NsJailPath); err != nil {
			log.Fatalf("nsjail enabled but not found at '%s'", cfg.NsJailPath)
		}
	}
	if err := src.ValidateDirs(cfg); err != nil {
		log.Fatalf("invalid dirs: %v", err)
	}
	srv := src.NewServer(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
}
