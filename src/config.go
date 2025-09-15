package src

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		WorkingDir:            "/srv/emcc-sandboxd", // Default to standard service directory
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
		NsJailEnabled:       false,
		NsJailPath:          "nsjail",
		CgroupV2Root:        "cgroup",
		EnableResourceGating: false,
		JobMemoryEstimateMB:  256,
	}
}

// LoadConfig loads configuration from a file, falling back to defaults
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
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

// ValidateDirs validates the configuration directories
func ValidateDirs(cfg Config) error {
	if cfg.BaseDir == "" {
		return fmt.Errorf("baseDir empty")
	}
	// Ensure base dir exists
	if err := os.MkdirAll(cfg.BaseDir, 0o755); err != nil {
		return err
	}
	return nil
}