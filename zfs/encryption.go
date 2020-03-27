package zfs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/zfs/zfscmd"
)

var encryptionCLISupport struct {
	once      sync.Once
	supported bool
	err       error
}

func EncryptionCLISupported(ctx context.Context) (bool, error) {
	encryptionCLISupport.once.Do(func() {
		// "feature discovery"
		cmd := zfscmd.CommandContext(ctx, "zfs", "load-key")
		output, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); !ok || ok && !ee.Exited() {
			encryptionCLISupport.err = errors.Wrap(err, "native encryption cli support feature check failed")
		}
		def := strings.Contains(string(output), "load-key") && strings.Contains(string(output), "keylocation")
		encryptionCLISupport.supported = envconst.Bool("ZREPL_EXPERIMENTAL_ZFS_ENCRYPTION_CLI_SUPPORTED", def)
		debug("encryption cli feature check complete %#v", &encryptionCLISupport)
	})
	return encryptionCLISupport.supported, encryptionCLISupport.err
}

// returns false, nil if encryption is not supported
func ZFSGetEncryptionEnabled(ctx context.Context, fs string) (enabled bool, err error) {
	defer func(e *error) {
		if *e != nil {
			*e = fmt.Errorf("zfs get encryption enabled fs=%q: %s", fs, *e)
		}
	}(&err)
	if supp, err := EncryptionCLISupported(ctx); err != nil {
		return false, err
	} else if !supp {
		return false, nil
	}

	if err := validateZFSFilesystem(fs); err != nil {
		return false, err
	}

	props, err := zfsGet(ctx, fs, []string{"encryption"}, sourceAny)
	if err != nil {
		return false, errors.Wrap(err, "cannot get `encryption` property")
	}
	val := props.Get("encryption")
	switch val {
	case "":
		panic("zfs get should return a value for `encryption`")
	case "-":
		return false, errors.New("`encryption` property should never be \"-\"")
	case "off":
		return false, nil
	default:
		// we don't want to hardcode the cipher list, and we checked for != 'off'
		// ==> assume any other value means encryption is enabled
		// TODO add test to OpenZFS test suite
		return true, nil
	}
}
