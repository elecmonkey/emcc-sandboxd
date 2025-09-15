package src

import "time"

// Config holds all configuration for the emcc-sandboxd service
type Config struct {
	WorkingDir            string        `json:"workingDir"` // Working directory for the service, defaults to current dir
	Addr                  string        `json:"addr"`
	BaseDir               string        `json:"baseDir"`
	JobsDir               string        `json:"jobsDir"`
	ArtifactsDir          string        `json:"artifactsDir"`
	EnableStaticArtifacts bool          `json:"enableStaticArtifacts"`
	ArtifactTTL           time.Duration `json:"-"`
	ArtifactTTLDays       int           `json:"artifactTTLDays"`
	CleanupIntervalMins   int           `json:"cleanupIntervalMins"`
	DefaultArgs           []string      `json:"defaultArgs"`
	NsJailEnabled         bool          `json:"nsjailEnabled"`
	NsJailPath            string        `json:"nsjailPath"`
	CgroupV2Root          string        `json:"cgroupV2Root"`
	EnableResourceGating  bool          `json:"enableResourceGating"`
	JobMemoryEstimateMB   int64         `json:"jobMemoryEstimateMB"`
}

// CompileRequest represents the request payload for compilation
type CompileRequest struct {
	Code string   `json:"code"`
	Type string   `json:"type"` // "c" or "cpp"
	Args []string `json:"args"`
}

// CompileResponse represents the response from compilation
type CompileResponse struct {
	OK    bool   `json:"ok"`
	ID    string `json:"id"`
	JS    string `json:"js"`
	WASM  string `json:"wasm"`
	Error string `json:"error,omitempty"`
}
