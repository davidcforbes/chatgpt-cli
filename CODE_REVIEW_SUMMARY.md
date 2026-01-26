# Code Review Summary - ChatGPT CLI

**Review Date:** 2026-01-25
**Reviewer:** Claude Code (feature-dev:code-reviewer agent)
**Focus Areas:** Security, Stability, Performance, Usability

## Executive Summary

A comprehensive security and code quality review was conducted on the ChatGPT CLI codebase. **7 high-confidence issues** were identified, ranging from critical security vulnerabilities to performance concerns.

### Issue Severity Breakdown

- **Critical (P0):** 2 issues
- **High (P1):** 2 issues
- **Medium (P2):** 3 issues

All identified issues have been recorded in the beads issue tracker with appropriate labels and priorities.

## Critical Issues (P0)

### 1. Command Injection Vulnerability in MCP STDIO Transport
**Issue ID:** chatgpt-cli-699
**File:** api/client/mcp.go (Lines 363-376)
**Labels:** security, critical
**Confidence:** 100%

The `splitCommandLine` function doesn't prevent shell injection patterns. An attacker controlling the MCP endpoint can execute arbitrary commands via semicolons, pipes, backticks, or command substitution.

**Attack Vector:**
```bash
chatgpt --mcp "stdio:malicious-cmd; rm -rf /" --mcp-tool test "query"
```

**Recommended Fix:**
- Implement whitelist for allowed MCP STDIO commands
- Validate command paths are absolute and within allowed directories
- Reject arguments containing shell metacharacters
- Use safer subprocess execution with explicit argument separation

### 2. Path Traversal Vulnerability in Agent File Operations
**Issue ID:** chatgpt-cli-bib
**File:** agent/core/policy.go (Lines 188-216)
**Labels:** security, critical
**Confidence:** 95%

The `escapesWorkDir` function can be bypassed via symbolic links and Windows-specific paths (UNC paths, drive letters). `filepath.Abs` doesn't resolve symlinks, allowing agents to access files outside the working directory.

**Attack Vector:**
```bash
# Create symlink: ln -s /etc workdir/etc_link
# Agent can then: file_read("etc_link/passwd")
```

**Recommended Fix:**
- Use `filepath.EvalSymlinks` to resolve symlinks before path comparison
- Add explicit validation for UNC paths and Windows drive letters
- Consider OS-level sandboxing (chroot, containers) for agent execution

## High Priority Issues (P1)

### 3. Sensitive Data Exposure via Insecure File Permissions
**Issue ID:** chatgpt-cli-2tw
**Files:** history/store.go (Line 82), config/store.go (Line 180), cmd/chatgpt/utils/utils.go (Line 75), cmd/chatgpt/main.go (Line 1038)
**Labels:** security, high
**Confidence:** 90%

Multiple locations write sensitive data with world-readable permissions (0644). History files contain complete conversation transcripts (potentially including API keys, passwords, sensitive business data). Config files may contain API keys.

**Impact:** On multi-user systems, any local user can read API keys, conversation history, and agent execution logs.

**Recommended Fix:**
- Change all sensitive file writes to mode 0600 (user read/write only)
- Apply to: history files, config files, cache files, agent logs

### 4. Unvalidated User Input in Shell Command Arguments
**Issue ID:** chatgpt-cli-81w
**File:** agent/core/policy.go (Lines 122-148)
**Labels:** security, high
**Confidence:** 85%

The `denyShellArgsOutsideWorkDir` function has multiple bypass vulnerabilities:
- Only checks if argument *starts* with `~` (not mid-string)
- Doesn't block shell variable expansion (`$HOME`, `${VAR}`, `%USERPROFILE%`)
- Glob patterns can expand to paths outside workdir
- Only validates arguments that "look like paths"

**Attack Vector:**
```bash
shell_exec("cat", ["--file=$HOME/.ssh/id_rsa"])
shell_exec("grep", ["secret", "$HOME/.*"])
```

**Recommended Fix:**
- Block all shell variable expansion patterns
- Expand globs and validate each resulting path
- Use allowlist approach for known-safe argument patterns
- Run shell commands in restricted environment

## Medium Priority Issues (P2)

### 5. Race Condition in Config File Updates
**Issue ID:** chatgpt-cli-600
**File:** cmd/chatgpt/main.go (Lines 1041-1080)
**Labels:** stability, medium
**Confidence:** 85%

The `saveConfig` function has a TOCTOU (time-of-check-to-time-of-use) race condition. Between checking if the config file exists and reading/writing it, another process could modify it, leading to lost updates or corruption.

**Recommended Fix:**
- Use atomic write pattern: write to temp file, then atomic rename
- Implement file locking (`syscall.Flock` on Unix)
- Add retry logic with exponential backoff

### 6. Missing Input Validation for API Key File Path
**Issue ID:** chatgpt-cli-r6o
**File:** cmd/chatgpt/main.go (Lines 300-310)
**Labels:** security, medium
**Confidence:** 85%

The API key file path from `--set-api-key-file` is not validated before reading. No size limit check, no path validation, error messages may leak file existence.

**Impact:**
- Information disclosure (file existence probing)
- Potential DoS via reading large files (e.g., `/dev/zero`)
- Could read sensitive mounted files in containerized environments

**Recommended Fix:**
- Validate path is within user's home directory or allowed locations
- Add file size check (max 10KB for API key)
- Sanitize error messages to avoid information leakage

### 7. Unbounded Memory Allocation in Agent Transcript
**Issue ID:** chatgpt-cli-smw
**File:** agent/core/runner.go (Lines 13, 536-541)
**Labels:** performance, stability, medium
**Confidence:** 80%

Transcript truncation is only applied *after* full string construction. Commands with large output (e.g., `find / -name "*"` or `cat /dev/urandom`) can cause out-of-memory errors.

**Recommended Fix:**
- Implement streaming truncation during construction
- Add output size limits at shell execution time
- Use `io.LimitReader` when reading command output

## Review Methodology

The review used the feature-dev:code-reviewer agent with confidence-based filtering (≥70% confidence threshold). Key areas reviewed:

- **cmd/chatgpt/main.go** - Entry point, flag handling
- **agent/** - Agent execution, policy enforcement, budget tracking
- **api/** - API clients, MCP handling
- **config/** - Configuration management
- **history/** and **cache/** - Data persistence

## Recommendations

### Immediate Actions (Critical/High)
1. ✅ Fix file permissions on all sensitive data writes (0600 instead of 0644)
2. ✅ Implement proper input validation for MCP STDIO endpoints
3. ✅ Add symlink resolution to agent path traversal checks
4. ✅ Block shell variable expansion in agent command arguments

### Short Term (Medium)
5. Add file locking for concurrent config updates
6. Validate API key file paths and add size limits
7. Implement streaming truncation for agent transcripts

### Long Term Considerations
- Add OS-level sandboxing for agent mode (containers, seccomp, AppArmor)
- Implement comprehensive input validation framework
- Add security-focused integration tests
- Consider security audit by external firm for agent mode

## Positive Security Practices Observed

The codebase demonstrates good security awareness:
- ✅ Budget limits for agent execution (steps, tokens, time)
- ✅ Policy enforcement framework (allowed tools, denied commands)
- ✅ Dry-run mode for testing agent behavior
- ✅ Structured logging with audit trails
- ✅ Configuration validation and error handling

## Issue Tracking

All identified issues are tracked in beads:

```bash
bd list --type bug --pretty
```

To view security-specific issues:

```bash
bd list -l security --pretty
```

To view critical issues:

```bash
bd list --priority 0 --pretty
```

## Next Steps

1. Review and prioritize issues based on deployment context
2. Assign issues to developers
3. Implement fixes with appropriate test coverage
4. Security review of fixes before merging
5. Consider adding security-focused CI/CD checks

---

**Note:** This review focused on high-confidence issues. A deeper security audit may reveal additional concerns, particularly around the agent execution model and third-party integrations.
