package hooks

import (
	"context"
	"fmt"

	"github.com/LyingCak3/zrepl/internal/daemon/filters"
	"github.com/LyingCak3/zrepl/internal/zfs"
)

type HookJobCallback func(ctx context.Context) error

type CallbackHook struct {
	cb            HookJobCallback
	filter        Filter
	displayString string
}

func NewCallbackHookForFilesystem(displayString string, fs *zfs.DatasetPath, cb HookJobCallback) *CallbackHook {
	filter, _ := filters.DatasetMapFilterFromConfig(map[string]bool{fs.ToString(): true})
	return NewCallbackHook(displayString, cb, filter)
}

func NewCallbackHook(displayString string, cb HookJobCallback, filter Filter) *CallbackHook {
	return &CallbackHook{
		cb:            cb,
		filter:        filter,
		displayString: displayString,
	}
}

func (h *CallbackHook) Filesystems() Filter {
	return h.filter
}

func (h *CallbackHook) ErrIsFatal() bool {
	return false // callback is by definition
}

func (h *CallbackHook) String() string {
	return h.displayString
}

type CallbackHookReport struct {
	Name string
	Err  error
}

func (r *CallbackHookReport) String() string {
	if r.HadError() {
		return r.Error()
	}
	return r.Name
}

func (r *CallbackHookReport) HadError() bool { return r.Err != nil }

func (r *CallbackHookReport) Error() string {
	return fmt.Sprintf("%s error: %s", r.Name, r.Err)
}

func (h *CallbackHook) Run(ctx context.Context, edge Edge, phase Phase, dryRun bool, extra Env, state map[interface{}]interface{}) HookReport {
	err := h.cb(ctx)
	return &CallbackHookReport{h.displayString, err}
}
