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
// When direct is false (normal mode), zfs/zpool symlink to the current
// executable (interposer mode: validates commands and delegates via sudo).
// Additional .real symlinks point to the actual binaries.
//
// When direct is true, zfs/zpool symlink directly to the real binaries
// (no interposer, no sudo). Use this when running as root.
//
// Returns the directory path and a cleanup function.
func SetupInterposerPath(explicitDir string, direct bool) (string, func(), error) {
	// Resolve real binary paths while original PATH is still intact.
	// In direct mode we only need zfs/zpool and bash; in interposer
	// mode we also need sudo.
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
	// Allow subcommands that don't reference a specific dataset,
	// used for feature detection (e.g., "zfs send" with no dataset,
	// "zfs receive" with no dataset, "zpool get").
	if len(args) == 0 {
		return nil
	}

	// Some subcommands are inherently safe / don't target a pool.
	// Allow them unconditionally. The real safety net is that we
	// only have sudo for zfs/zpool, not for anything destructive
	// outside the test pool.
	//
	// Check if any non-flag argument references the pool name.
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if arg == poolName || strings.HasPrefix(arg, poolName+"/") || strings.HasPrefix(arg, poolName+"@") || strings.HasPrefix(arg, poolName+"#") {
			return nil
		}
	}

	// Block commands with -a (all) flag when no pool name was found.
	// These operate on ALL pools/filesystems (e.g., zfs unmount -a,
	// zpool export -a) and must not be allowed without a pool reference.
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		// -a standalone or in combined flags (e.g., -fa, -ra)
		if arg == "-a" || (len(arg) > 2 && arg[0] == '-' && arg[1] != '-' &&
			strings.ContainsRune(arg[1:], 'a')) {
			return fmt.Errorf("%s command rejected: -a flag not allowed without explicit pool reference", command)
		}
	}

	// Special case: bare subcommand with only flags (feature detection).
	// Count non-flag args. If there's only one (the subcommand itself),
	// allow it.
	//
	// Note: -s is intentionally NOT in the skip list. It is boolean in
	// some subcommands (e.g., zfs recv -s) and argument-taking in others
	// (e.g., zfs list -s property). Not skipping its "value" is safe:
	// commands with the pool name are already handled by the loop above,
	// and for commands without a pool name, over-counting non-flag args
	// causes a safe rejection rather than a dangerous acceptance.
	nonFlagArgs := 0
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		// Flags that take a value argument: -o, -O always; -t for zfs only.
		// -t is arg-taking in zfs (list -t type, send -t token) but
		// boolean in zpool (import -t = temporary, events -t = timestamps).
		argTakingChars := "oO"
		if command == "zfs" {
			argTakingChars = "oOt"
		}
		if arg == "-o" || arg == "-O" || (command == "zfs" && arg == "-t") ||
			(len(arg) > 2 && arg[0] == '-' && arg[1] != '-' &&
				strings.ContainsAny(arg, argTakingChars)) {
			skipNext = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		nonFlagArgs++
	}
	// If only the subcommand is present (e.g., "send", "receive", "get"),
	// this is likely feature detection.
	if nonFlagArgs <= 1 {
		return nil
	}

	return fmt.Errorf("%s command rejected: args %v do not reference pool %q", command, args, poolName)
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
