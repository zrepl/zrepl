package tests

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/pruner.v2"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/pruning"
	"github.com/zrepl/zrepl/zfs"
)

func Pruner2NotReplicated(ctx *platformtest.Context) {

	platformtest.Run(ctx, platformtest.PanicErr, ctx.RootDataset, `
        DESTROYROOT
        CREATEROOT
        +  "foo bar"
        +  "foo bar@1"
        +  "foo bar@2"
        +  "foo bar@3"
        +  "foo bar@4"
        +  "foo bar@5"
	`)

	fs := ctx.RootDataset + "/foo bar"
	senderJid := endpoint.MustMakeJobID("sender-job")
	otherJid1 := endpoint.MustMakeJobID("other-job-1")
	otherJid2 := endpoint.MustMakeJobID("other-job-2")


	// create step holds for the incremental @2->@3
	endpoint.HoldStep(ctx, fs, fsversion(ctx, fs, "@2"), senderJid)
	endpoint.HoldStep(ctx, fs, fsversion(ctx, fs, "@3"), senderJid)
	// create step holds for other-job-1 @2 -> @3
	endpoint.HoldStep(ctx, fs, fsversion(ctx, fs, "@2"), otherJid1)
	endpoint.HoldStep(ctx, fs, fsversion(ctx, fs, "@3"), otherJid1)
	// create step hold for other-job-2 @1 (will be pruned)
	endpoint.HoldStep(ctx, fs, fsversion(ctx, fs, "@1"), otherJid2)

	c, err := config.ParseConfigBytes([]byte(fmt.Sprintf(`
jobs:
- name: prunetest
  type: push
  filesystems: {
      "%s/foo bar": true
  }
  connect:
    type: tcp
    address: 255.255.255.255:255
  snapshotting:
    type: manual
  pruning:
    keep_sender:
    #- type: not_replicated
    - type: step_holds
    - type: last_n
      count: 1
    keep_receiver:
    - type: last_n
      count: 2
    `, ctx.RootDataset)))
	require.NoError(ctx, err)

	pushJob := c.Jobs[0].Ret.(*config.PushJob)

	require.NoError(ctx, err)

	fsfilter, err := filters.DatasetMapFilterFromConfig(pushJob.Filesystems)
	require.NoError(ctx, err)

	matchedFilesystems, err := zfs.ZFSListMapping(ctx, fsfilter)
	ctx.Logf("%s", pretty.Sprint(matchedFilesystems))
	require.NoError(ctx, err)
	require.Len(ctx, matchedFilesystems, 1)

	sideSender := pruner.NewSideSender(senderJid)

	keepRules, err := pruning.RulesFromConfig(senderJid, pushJob.Pruning.KeepSender)
	require.NoError(ctx, err)
	p := pruner.NewPruner(fsfilter, senderJid, sideSender, keepRules)

	runDone := make(chan *pruner.Report)
	go func() {
		runDone <- p.Run(ctx)
	}()

	var report *pruner.Report
	// concurrency stress
out:
	for {
		select {
		case <-time.After(10 * time.Millisecond):
			p.Report(ctx)
		case report = <-runDone:
			break out
		}
	}
	ctx.Logf("%s\n", pretty.Sprint(report))

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	require.NoError(ctx, err)
	ctx.Logf("%s\n", string(reportJSON))

	ctx.FailNow()
	// fs := ctx.RootDataset + "/foo bar"

	// // create a replication cursor to make pruning work at all
	// _, err = endpoint.CreateReplicationCursor(ctx, fs, fsversion(ctx, fs, "@2"), senderJid)
	// require.NoError(ctx, err)

	// p := prunerFactory.BuildSenderPruner(ctx, sender, sender)

	// p.Prune()

	// report := p.Report()

	// require.Equal(ctx, pruner.Done.String(), report.State)
	// require.Len(ctx, report.Completed, 1)
	// fsReport := report.Completed[0]
	// require.Equal(ctx, fs, fsReport.Filesystem)
	// require.Empty(ctx, fsReport.SkipReason)
	// require.Empty(ctx, fsReport.LastError)
	// require.Len(ctx, fsReport.DestroyList, 1)
	// require.Equal(ctx, fsReport.DestroyList[0], pruner.SnapshotReport{
	// 	Name:       "1",
	// 	Replicated: true,
	// 	Date:       fsReport.DestroyList[0].Date,
	// })

}
