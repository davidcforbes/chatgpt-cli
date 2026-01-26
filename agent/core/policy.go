package core

import (
	"errors"
	"fmt"
	"github.com/kardolus/chatgpt-cli/agent/types"
	"go.uber.org/zap"
	"path/filepath"
	"strings"
)

type Policy interface {
	AllowStep(cfg types.Config, step types.Step) error
}

const (
	PolicyKindStepType   = "step_type"
	PolicyKindShell      = "shell"
	PolicyKindLLM        = "llm"
	PolicyKindFiles      = "files"
	PolicyKindPathEscape = "path_escape"
)

type DefaultPolicy struct {
	limits PolicyLimits
}

type PolicyLimits struct {
	AllowedTools           []types.ToolKind
	RestrictFilesToWorkDir bool
	DeniedShellCommands    []string
	AllowedFileOps         []string
}

func NewDefaultPolicy(limits PolicyLimits) *DefaultPolicy {
	return &DefaultPolicy{limits: limits}
}

func (p *DefaultPolicy) AllowStep(cfg types.Config, step types.Step) error {
	switch step.Type {
	case types.ToolShell, types.ToolLLM, types.ToolFiles:
		// ok
	default:
		return PolicyDeniedError{
			Kind:   PolicyKindStepType,
			Reason: fmt.Sprintf("unsupported step type: %s", step.Type),
		}
	}

	if len(p.limits.AllowedTools) > 0 && !containsTool(p.limits.AllowedTools, step.Type) {
		return PolicyDeniedError{
			Kind:   PolicyKindStepType,
			Reason: fmt.Sprintf("tool not allowed: %s", step.Type),
		}
	}

	switch step.Type {
	case types.ToolShell:
		cmd := strings.TrimSpace(step.Command)
		if cmd == "" {
			return PolicyDeniedError{Kind: PolicyKindShell, Reason: "shell step requires Command"}
		}
		if len(p.limits.DeniedShellCommands) > 0 && containsString(p.limits.DeniedShellCommands, cmd) {
			return PolicyDeniedError{Kind: PolicyKindShell, Reason: fmt.Sprintf("shell command denied: %s", cmd)}
		}

		if p.limits.RestrictFilesToWorkDir && cfg.WorkDir != "" {
			if err := denyShellArgsOutsideWorkDir(cfg.WorkDir, step.Args); err != nil {
				return err
			}
		}

	case types.ToolLLM:
		if strings.TrimSpace(step.Prompt) == "" {
			return PolicyDeniedError{Kind: PolicyKindLLM, Reason: "llm step requires Prompt"}
		}

	case types.ToolFiles:
		op := strings.ToLower(strings.TrimSpace(step.Op))
		if op == "" {
			return PolicyDeniedError{Kind: PolicyKindFiles, Reason: "file step requires Op"}
		}
		if strings.TrimSpace(step.Path) == "" {
			return PolicyDeniedError{Kind: PolicyKindFiles, Reason: "file step requires Path"}
		}

		switch op {
		case "patch":
			if strings.TrimSpace(step.Data) == "" {
				return PolicyDeniedError{
					Kind:   PolicyKindFiles,
					Reason: "patch requires Data (unified diff)",
				}
			}
		case "replace":
			if len(step.Old) == 0 {
				return PolicyDeniedError{
					Kind:   PolicyKindFiles,
					Reason: "replace requires Old pattern",
				}
			}
		}

		if !fileOpAllowed(p.limits.AllowedFileOps, op) {
			return PolicyDeniedError{Kind: PolicyKindFiles, Reason: fmt.Sprintf("file op not allowed: %s", op)}
		}

		if p.limits.RestrictFilesToWorkDir && cfg.WorkDir != "" {
			if escapesWorkDir(cfg.WorkDir, step.Path) {
				return PolicyDeniedError{
					Kind:   PolicyKindPathEscape,
					Reason: fmt.Sprintf("path escapes workdir: workdir=%q path=%q", cfg.WorkDir, step.Path),
				}
			}
		}
	}

	return nil
}

// denyShellArgsOutsideWorkDir blocks absolute paths, ~, .., shell variables, and any arg that would escape workdir.
func denyShellArgsOutsideWorkDir(workdir string, args []string) error {
	for _, raw := range args {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}

		// If it contains path separators or looks like a path, validate it first.
		// (This is intentionally conservative; better to block than allow rm /tmp.)
		// Check this BEFORE blocking individual patterns so we get consistent "escapes workdir" errors.
		if strings.Contains(s, "/") || strings.Contains(s, `\`) || strings.Contains(s, "..") || filepath.IsAbs(s) {
			if escapesWorkDir(workdir, s) {
				return PolicyDeniedError{
					Kind:   PolicyKindPathEscape,
					Reason: fmt.Sprintf("shell arg escapes workdir: workdir=%q arg=%q", workdir, s),
				}
			}
		}

		// Block shell variable expansion patterns (Unix and Windows)
		dangerousPatterns := []string{
			"$",      // Unix shell variables: $HOME, $(cmd), ${VAR}
			"`",      // Command substitution
			"%",      // Windows environment variables: %USERPROFILE%
		}
		for _, pattern := range dangerousPatterns {
			if strings.Contains(s, pattern) {
				return PolicyDeniedError{
					Kind:   PolicyKindPathEscape,
					Reason: fmt.Sprintf("shell arg contains forbidden pattern %q: workdir=%q arg=%q", pattern, workdir, s),
				}
			}
		}

		// Block glob patterns that could expand to paths outside workdir
		if strings.ContainsAny(s, "*?[") {
			return PolicyDeniedError{
				Kind:   PolicyKindPathEscape,
				Reason: fmt.Sprintf("shell arg contains glob pattern: workdir=%q arg=%q", workdir, s),
			}
		}
	}
	return nil
}

// PolicyDeniedError is a typed error so Agent/Planner can branch on it.
type PolicyDeniedError struct {
	Kind   string
	Reason string
}

func (e PolicyDeniedError) Error() string {
	return fmt.Sprintf("policy denied: kind=%s reason=%s", e.Kind, e.Reason)
}

func IsPolicyStop(err error, log *zap.SugaredLogger) bool {
	var pe PolicyDeniedError
	if errors.As(err, &pe) {
		log.Warnf("Policy denied (kind=%s): %v", pe.Kind, err)
		return true
	}
	return false
}

func containsTool(xs []types.ToolKind, k types.ToolKind) bool {
	for _, x := range xs {
		if x == k {
			return true
		}
	}
	return false
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// escapesWorkDir returns true if path, when resolved relative to workdir, is outside workdir.
// This function now properly handles symlinks and Windows-specific paths (UNC, drive letters).
func escapesWorkDir(workdir, path string) bool {
	// Check for Windows UNC paths in the input path
	if isWindowsUNCPath(path) {
		return true // UNC paths are not allowed
	}

	// Check for tilde expansion (~) which should be blocked cross-platform
	if len(path) > 0 && path[0] == '~' {
		return true // Home directory expansion is not allowed
	}

	// On Windows, if both workdir and path are Unix-style (start with /), compare them as Unix paths
	// This handles cases like workdir="/repo" path="/etc/passwd" which should be an escape
	if filepath.Separator == '\\' && len(workdir) > 0 && workdir[0] == '/' && len(path) > 0 && path[0] == '/' {
		// Clean the Unix-style paths
		cleanWd := filepath.ToSlash(filepath.Clean(workdir))
		cleanPath := filepath.ToSlash(filepath.Clean(path))

		// Check if path is inside workdir using Unix path logic
		if cleanPath == cleanWd {
			return false
		}
		prefix := cleanWd + "/"
		if strings.HasPrefix(cleanPath, prefix) {
			return false // Path is inside workdir
		}
		return true // Path is outside workdir
	}

	// Resolve workdir to absolute path
	wd, err := filepath.Abs(workdir)
	if err != nil {
		return true // fail closed
	}
	wd = filepath.Clean(wd)

	// Try to resolve symlinks in workdir if it exists
	if wdResolved, err := filepath.EvalSymlinks(wd); err == nil {
		wd = wdResolved
	}

	// On Windows, if path starts with / but workdir is a real Windows path (not Unix-style),
	// block the path immediately as it's suspicious cross-platform usage
	// This catches /tmp, /etc/passwd when workdir is something like C:\repo or .
	if filepath.Separator == '\\' && len(path) > 0 && path[0] == '/' {
		// Workdir was already resolved, if it has a volume name it's a real Windows path
		if filepath.VolumeName(wd) != "" {
			return true // Block Unix-style paths when workdir is Windows-native
		}
	}

	// Resolve target path
	var full string
	if filepath.IsAbs(path) {
		full = path
	} else {
		full = filepath.Join(wd, path)
	}
	full, err = filepath.Abs(full)
	if err != nil {
		return true // fail closed
	}
	full = filepath.Clean(full)

	// Try to resolve symlinks if path exists
	// For non-existent paths, use the cleaned absolute path
	resolved := full
	if resolvedPath, err := filepath.EvalSymlinks(full); err == nil {
		resolved = resolvedPath
	}
	resolved = filepath.Clean(resolved)

	// On Windows, check if the resolved path is on a different drive than workdir
	if isWindowsDifferentDrive(wd, resolved) {
		return true
	}

	// Allow exactly wd or anything under wd
	if resolved == wd {
		return false
	}
	prefix := wd + string(filepath.Separator)
	return !strings.HasPrefix(resolved, prefix)
}

// isWindowsUNCPath checks if a path is a Windows UNC path (\\server\share)
func isWindowsUNCPath(path string) bool {
	return len(path) >= 2 && path[0] == '\\' && path[1] == '\\'
}

// isWindowsDifferentDrive checks if two paths are on different Windows drives
func isWindowsDifferentDrive(path1, path2 string) bool {
	vol1 := filepath.VolumeName(path1)
	vol2 := filepath.VolumeName(path2)

	// If both have volume names (e.g., "C:", "D:"), they must match
	if vol1 != "" && vol2 != "" {
		return !strings.EqualFold(vol1, vol2)
	}

	return false
}

func fileOpAllowed(allowed []string, op string) bool {
	if len(allowed) == 0 {
		return true
	}
	if containsString(allowed, op) {
		return true
	}
	// If you can write arbitrary bytes, you can also patch/replace.
	if (op == "patch" || op == "replace") && containsString(allowed, "write") {
		return true
	}
	return false
}
