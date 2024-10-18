package zfs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/internal/util/envconst"
	"github.com/zrepl/zrepl/internal/zfs/zfscmd"
)

// no need for feature tests, holds have been around forever

func validateNotEmpty(field, s string) error {
	if s == "" {
		return fmt.Errorf("`%s` must not be empty", field)
	}
	return nil
}

// returned err != nil is guaranteed to represent invalid hold tag
func ValidHoldTag(tag string) error {
	maxlen := envconst.Int("ZREPL_ZFS_MAX_HOLD_TAG_LEN", 256-1) // 256 include NULL byte, from module/zfs/dsl_userhold.c
	if len(tag) > maxlen {
		return fmt.Errorf("hold tag %q exceeds max length of %d", tag, maxlen)
	}
	return nil
}

// Idemptotent: does not return an error if the tag already exists
func ZFSHold(ctx context.Context, fs string, v FilesystemVersion, tag string) error {
	if !v.IsSnapshot() {
		return errors.Errorf("can only hold snapshots, got %s", v.RelName())
	}

	if err := validateNotEmpty("tag", tag); err != nil {
		return err
	}
	fullPath := v.FullPath(fs)
	output, err := zfscmd.CommandContext(ctx, "zfs", "hold", tag, fullPath).CombinedOutput()
	if err != nil {
		if bytes.Contains(output, []byte("tag already exists on this dataset")) {
			goto success
		}
		return &ZFSError{output, errors.Wrapf(err, "cannot hold %q", fullPath)}
	}
success:
	return nil
}

func ZFSHolds(ctx context.Context, fs, snap string) ([]string, error) {
	if err := validateZFSFilesystem(fs); err != nil {
		return nil, errors.Wrap(err, "`fs` is not a valid filesystem path")
	}
	if snap == "" {
		return nil, fmt.Errorf("`snap` must not be empty")
	}
	dp := fmt.Sprintf("%s@%s", fs, snap)
	output, err := zfscmd.CommandContext(ctx, "zfs", "holds", "-H", dp).CombinedOutput()
	if err != nil {
		return nil, &ZFSError{output, errors.Wrap(err, "zfs holds failed")}
	}
	scan := bufio.NewScanner(bytes.NewReader(output))
	var tags []string
	for scan.Scan() {
		// NAME              TAG  TIMESTAMP
		comps := strings.SplitN(scan.Text(), "\t", 3)
		if len(comps) != 3 {
			return nil, fmt.Errorf("zfs holds: unexpected output\n%s", output)
		}
		if comps[0] != dp {
			return nil, fmt.Errorf("zfs holds: unexpected output: expecting %q as first component, got %q\n%s", dp, comps[0], output)
		}
		tags = append(tags, comps[1])
	}
	return tags, nil
}

// Idempotent: if the hold doesn't exist, this is not an error
func ZFSRelease(ctx context.Context, tag string, snaps ...string) error {
	cumLens := make([]int, len(snaps))
	for i := 1; i < len(snaps); i++ {
		cumLens[i] = cumLens[i-1] + len(snaps[i])
	}
	maxInvocationLen := 12 * os.Getpagesize()
	var noSuchTagLines, otherLines []string
	for i := 0; i < len(snaps); {
		var j = i
		for ; j < len(snaps); j++ {
			if cumLens[j]-cumLens[i] > maxInvocationLen {
				break
			}
		}
		args := []string{"release", tag}
		args = append(args, snaps[i:j]...)
		output, err := zfscmd.CommandContext(ctx, "zfs", args...).CombinedOutput()
		if pe, ok := err.(*os.PathError); err != nil && ok && pe.Err == syscall.E2BIG {
			maxInvocationLen = maxInvocationLen / 2
			continue
		}
		// further error handling part of error scraper below

		maxInvocationLen = maxInvocationLen + os.Getpagesize()
		i = j

		// even if release fails for datasets where there's no hold with the tag
		// the hold is still released on datasets which have a hold with the tag
		// FIXME verify this in a platformtest
		// => screen-scrape
		scan := bufio.NewScanner(bytes.NewReader(output))
		for scan.Scan() {
			line := scan.Text()
			if strings.Contains(line, "no such tag on this dataset") {
				noSuchTagLines = append(noSuchTagLines, line)
			} else {
				otherLines = append(otherLines, line)
			}
		}

	}
	if debugEnabled {
		debug("zfs release: no such tag lines=%v otherLines=%v", noSuchTagLines, otherLines)
	}
	if len(otherLines) > 0 {
		return fmt.Errorf("unknown zfs error while releasing hold with tag %q:\n%s", tag, strings.Join(otherLines, "\n"))
	}
	return nil
}
