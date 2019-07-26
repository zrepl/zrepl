// Package hooks implements pre- and post snapshot hooks.
//
// Plan is a generic executor for ExpectStepReports before and after an activity specified in a callback.
// It provides a reporting facility that can be polled while the plan is executing to gather progress information.
//
// This package also provides all supported hook type implementations and abstractions around them.
//
// Use For Other Kinds Of ExpectStepReports
//
// This package REQUIRES REFACTORING before it can be used for other activities than snapshots, e.g. pre- and post-replication:
//
// The Hook interface requires a hook to provide a Filesystems() filter, which doesn't make sense for
// all kinds of activities.
//
// The hook implementations should move out of this package.
// However, there is a lot of tight coupling which to untangle isn't worth it ATM.
//
// How This Package Is Used By Package Snapper
//
// Deserialize a config.List using ListFromConfig().
// Then it MUST filter the list to only contain hooks for a particular filesystem using
// hooksList.CopyFilteredForFilesystem(fs).
//
// Then create a CallbackHook using NewCallbackHookForFilesystem().
//
// Pass all of the above to NewPlan() which provides a Report() and Run() method:
//
// Plan.Run(ctx context.Context,dryRun bool) executes the plan and take a context as argument that should contain a logger added using hooks.WithLogger()).
// The value of dryRun is passed through to the hooks' Run() method.
// Command hooks make it available in the environment variable ZREPL_DRYRUN.
//
// Plan.Report() can be called while Plan.Run() is executing to give an overview of plan execution progress (future use in "zrepl status").
//
package hooks
