package src

import "strings"

// MergeAndFilterArgs merges default args with user args, filtering by whitelist
func (s *Server) MergeAndFilterArgs(user []string) []string {
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

// isBlockedArg checks if an argument is in the blocked list
func isBlockedArg(a string, blocked []string) bool {
	for _, b := range blocked {
		if a == b || strings.HasPrefix(a, b+"=") {
			return true
		}
	}
	return false
}

// isAllowedArg checks if an argument is in the allowed list
func isAllowedArg(a string, allowed []string) bool {
	// exact match or prefix match with '=' are both considered via prefix list
	for _, p := range allowed {
		if strings.HasPrefix(a, p) || a == p {
			return true
		}
	}
	return false
}

// safeArgPath validates that a path argument is safe
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