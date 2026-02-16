// # Safety Interposer
//
// The platformtest binary is a multi-personality binary (busybox-style).
// When invoked as "zfs" or "zpool" (via argv[0]), it acts as a safety
// interposer that validates commands and delegates to the real binary
// via sudo.
//
// SetupInterposerPath creates a temp directory with symlinks and replaces PATH:
//   - zfs/zpool → the platformtest binary itself (interposer mode)
//   - zfs.real/zpool.real → actual binaries (for delegation via sudo)
//   - sudo/bash → real binaries (so they remain accessible)
//
// When invoked as zfs or zpool, the binary validates that all commands
// reference the test pool "zreplplatformtest" before delegating through
// sudo. The validation rule is simple: the pool name must appear in the
// command args. Special cases:
//   - Resume tokens ("zfs send -t <token>"): decoded via the real binary
//     to extract and verify the toname field
//   - The -a (all) flag is blocked without an explicit pool reference
//   - Bare subcommands (feature detection) are allowed
//   - Mountpoint paths are validated against traversal on post-create chmod
//
// With -no-interposer, zfs/zpool symlink directly to the real binaries
// (no sudo, no validation). Use this when running as root.
package platformtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SetupInterposerPath creates a temp directory with zfs/zpool symlinks
// and replaces PATH so that all zfs/zpool invocations go through it.
//
// If `explicitDir` is non-empty, the PATH is constructed in that directory
// instead of a temp directory, and the caller is responsible for cleanup.
//
// `direct` controls whether to set up the interposer (false) or to symlink
// zfs/zpool directly to the real binaries (true, no interposer, no sudo).
//
// Returns the directory path and a cleanup function.
func SetupInterposerPath(explicitDir string, direct bool) (string, func(), error) {
	// Resolve real binary paths while original PATH is still intact.
	lookupNames := []string{"zfs", "zpool", "bash"}
	if !direct {
		lookupNames = append(lookupNames, "sudo")
	}
	realBinaries := make(map[string]string)
	for _, name := range lookupNames {
		p, err := exec.LookPath(name)
		if err != nil {
			return "", nil, fmt.Errorf("find real %s binary: %w", name, err)
		}
		realBinaries[name] = p
	}

	var dir string
	var cleanup func()

	if explicitDir != "" {
		dir = explicitDir
		cleanup = func() {} // caller manages lifecycle
	} else {
		var err error
		dir, err = os.MkdirTemp("", "zrepl-platformtest-*")
		if err != nil {
			return "", nil, fmt.Errorf("create interposer dir: %w", err)
		}
		cleanup = func() { os.RemoveAll(dir) }
	}

	if direct {
		// Direct mode: symlink zfs/zpool to real binaries (no interposer).
		for _, name := range []string{"zfs", "zpool", "bash"} {
			link := filepath.Join(dir, name)
			os.Remove(link)
			if err := os.Symlink(realBinaries[name], link); err != nil {
				cleanup()
				return "", nil, fmt.Errorf("create symlink %s: %w", name, err)
			}
		}
	} else {
		// Interposer mode: symlink zfs/zpool to self, .real to actual binaries.
		self, err := os.Executable()
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("get executable path: %w", err)
		}

		for _, name := range []string{"zfs", "zpool"} {
			link := filepath.Join(dir, name)
			os.Remove(link)
			if err := os.Symlink(self, link); err != nil {
				cleanup()
				return "", nil, fmt.Errorf("create symlink %s: %w", name, err)
			}
			realLink := filepath.Join(dir, name+".real")
			os.Remove(realLink)
			if err := os.Symlink(realBinaries[name], realLink); err != nil {
				cleanup()
				return "", nil, fmt.Errorf("create symlink %s.real: %w", name, err)
			}
		}

		// Symlink other required binaries so they remain accessible
		// after PATH replacement.
		for _, name := range []string{"sudo", "bash"} {
			link := filepath.Join(dir, name)
			os.Remove(link)
			if err := os.Symlink(realBinaries[name], link); err != nil {
				cleanup()
				return "", nil, fmt.Errorf("create symlink %s: %w", name, err)
			}
		}
	}

	// Replace PATH entirely so ALL zfs/zpool calls go through this directory.
	os.Setenv("PATH", dir)

	return dir, cleanup, nil
}

// RunInterposer handles the zfs/zpool interposer mode.
// It validates that the command references the test pool,
// then executes the real binary via sudo using the .real
// symlink placed by SetupInterposerPath.
func RunInterposer(command string) int {
	args := os.Args[1:]

	if err := validatePoolName(command, args, PlatformTestPoolName); err != nil {
		fmt.Fprintf(os.Stderr, "SAFETY: %v\n", err)
		return 1
	}

	// The .real symlink in PATH points to the actual binary.
	// sudo follows the symlink, so no explicit resolution needed.
	realBinary := filepath.Join(os.Getenv("PATH"), command+".real")
	sudoArgs := append([]string{realBinary}, args...)
	cmd := exec.Command("sudo", sudoArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()

	// After a successful create, chmod the mountpoint so the
	// unprivileged test user can operate on it.
	if err == nil && isCreateOperation(args) {
		if mountpoint := extractCreateMountpoint(command, args); mountpoint != "" {
			chmodMountpoint(mountpoint)
		}
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "interposer: exec error: %v\n", err)
		return 1
	}
	return 0
}

func validatePoolName(command string, args []string, poolName string) error {
	if len(args) == 0 {
		return nil
	}

	// Check if any non-flag argument references the pool name.
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if arg == poolName || strings.HasPrefix(arg, poolName+"/") || strings.HasPrefix(arg, poolName+"@") || strings.HasPrefix(arg, poolName+"#") {
			return nil
		}
	}

	// Special case: "zfs send -t <token>" (or -nvt, etc.) — the pool
	// name is encoded in the resume token, not visible in the args.
	// Decode the token via the real binary and verify toname.
	if command == "zfs" {
		if token := extractResumeToken(args); token != "" {
			return validateResumeTokenPool(token, poolName)
		}
	}

	// Block -a (all) flag without pool reference. Commands like
	// "zfs unmount -a" or "zpool export -a" affect all pools.
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		if arg == "-a" || (len(arg) > 2 && arg[0] == '-' && arg[1] != '-' &&
			strings.ContainsRune(arg[1:], 'a')) {
			return fmt.Errorf("%s command rejected: -a flag not allowed without explicit pool reference", command)
		}
	}

	// If only one non-flag arg is present (the subcommand itself),
	// this is feature detection (e.g., bare "zfs send", "zfs receive").
	nonFlagArgs := 0
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			nonFlagArgs++
		}
	}
	if nonFlagArgs <= 1 {
		return nil
	}

	return fmt.Errorf("%s command rejected: args %v do not reference pool %q", command, args, poolName)
}

// extractResumeToken returns the resume token from "zfs send -t <token>"
// or combined forms like "zfs send -nvt <token>". Returns "" if not found.
func extractResumeToken(args []string) string {
	if len(args) == 0 || args[0] != "send" {
		return ""
	}
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		// -t standalone, or combined flag containing t (e.g., -nvt)
		if arg == "-t" || (len(arg) > 2 && arg[0] == '-' && arg[1] != '-' &&
			strings.ContainsRune(arg[1:], 't')) {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return ""
}

// validateResumeTokenPool decodes a resume token by calling the real zfs
// binary directly (bypassing the interposer) and checks that the toname
// field references the expected pool.
func validateResumeTokenPool(token, poolName string) error {
	realBinary := filepath.Join(os.Getenv("PATH"), "zfs.real")
	cmd := exec.Command("sudo", realBinary, "send", "-nvt", token)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// zfs send -nvt may exit non-zero if the dataset no longer exists,
		// but it still prints the token contents. Only fail on exec errors.
		if _, ok := err.(*exec.ExitError); !ok {
			return fmt.Errorf("decode resume token: %w", err)
		}
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "toname = ") {
			toname := strings.TrimPrefix(line, "toname = ")
			if toname == poolName || strings.HasPrefix(toname, poolName+"/") || strings.HasPrefix(toname, poolName+"@") {
				return nil
			}
			return fmt.Errorf("resume token rejected: toname %q does not reference pool %q", toname, poolName)
		}
	}

	return fmt.Errorf("resume token rejected: could not extract toname from token")
}

func isCreateOperation(args []string) bool {
	return len(args) > 0 && args[0] == "create"
}

func extractCreateMountpoint(command string, args []string) string {
	switch command {
	case "zpool":
		// zpool create ... -O mountpoint=<path> ...
		for i, arg := range args {
			if arg == "-O" && i+1 < len(args) {
				if strings.HasPrefix(args[i+1], "mountpoint=") {
					return strings.TrimPrefix(args[i+1], "mountpoint=")
				}
			}
		}
	case "zfs":
		// zfs create [-p] [-o ...] <dataset>
		// Dataset is the last non-flag argument. Its mountpoint is
		// derived from the pool mountpoint + dataset path suffix.
		dataset := ""
		skipNext := false
		for _, arg := range args[1:] { // skip "create"
			if skipNext {
				skipNext = false
				continue
			}
			if arg == "-o" {
				skipNext = true
				continue
			}
			if strings.HasPrefix(arg, "-") {
				continue
			}
			dataset = arg
		}
		if dataset != "" && strings.HasPrefix(dataset, PlatformTestPoolName+"/") {
			suffix := strings.TrimPrefix(dataset, PlatformTestPoolName)
			return PlatformTestMountpoint + suffix
		}
	}
	return ""
}

func chmodMountpoint(mountpoint string) {
	// Validate: only chmod paths under the test mountpoint to prevent
	// path traversal (e.g., dataset "pool/../../etc" -> mountpoint "/etc").
	cleaned := filepath.Clean(mountpoint)
	if cleaned != PlatformTestMountpoint && !strings.HasPrefix(cleaned, PlatformTestMountpoint+"/") {
		fmt.Fprintf(os.Stderr, "interposer: refusing to chmod %q (not under %s)\n",
			mountpoint, PlatformTestMountpoint)
		return
	}
	// Best-effort: make mountpoint accessible to unprivileged user.
	exec.Command("sudo", "chmod", "0777", cleaned).Run() //nolint:errcheck
}
