package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/inexio/go-monitoringplugin"
	"github.com/spf13/cobra"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/version"
	"github.com/zrepl/zrepl/zfs"
)

var MonitorCmd = &cli.Subcommand{
	Use:   "monitor",
	Short: "Icinga/Nagios health checks",
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{newMonitorAliveCmd(), newMonitorSnapshotsCmd()}
	},
}

func newMonitorAliveCmd() *cli.Subcommand {
	runner := monitorAlive{}
	return &cli.Subcommand{
		Use:   "alive",
		Short: "check the daemon is alive",
		Run:   runner.run,
	}
}

func newMonitorSnapshotsCmd() *cli.Subcommand {
	runner := monitorSnapshots{}
	return &cli.Subcommand{
		Use:   "snapshots",
		Short: "check snapshots age",
		SetupSubcommands: func() []*cli.Subcommand {
			return []*cli.Subcommand{
				newLatestSnapshotsCmd(&runner),
				newOldestSnapshotsCmd(&runner),
			}
		},
		SetupCobra: func(c *cobra.Command) {
			f := c.PersistentFlags()
			f.StringVarP(&runner.job, "job", "j", "", "the name of the job")
			f.StringVarP(&runner.prefix, "prefix", "p", "", "snapshot prefix")
			f.DurationVarP(&runner.critical, "crit", "c", 0, "critical snapshot age")
			f.DurationVarP(&runner.warning, "warn", "w", 0, "warning snapshot age")

			_ = c.MarkFlagRequired("job")
			c.MarkFlagsRequiredTogether("prefix", "crit")
		},
	}
}

func newLatestSnapshotsCmd(runner *monitorSnapshots) *cli.Subcommand {
	return &cli.Subcommand{
		Use:   "latest",
		Short: "check latest snapshots are not too old, according to rules",
		Run:   runner.run,
	}
}

func newOldestSnapshotsCmd(runner *monitorSnapshots) *cli.Subcommand {
	return &cli.Subcommand{
		Use:   "oldest",
		Short: "check oldest snapshots are not too old, according to rules",
		Run: func(ctx context.Context, subcmd *cli.Subcommand, args []string) error {
			runner.oldest = true
			return runner.run(ctx, subcmd, args)
		},
	}
}

type monitorSnapshots struct {
	job      string
	oldest   bool
	prefix   string
	critical time.Duration
	warning  time.Duration
}

func (self *monitorSnapshots) run(
	ctx context.Context, subcmd *cli.Subcommand, args []string,
) error {
	jobConfig, err := subcmd.Config().Job(self.job)
	if err != nil {
		return err
	}

	datasets, rules, err := self.datasetsRules(ctx, jobConfig)
	if err != nil {
		return err
	} else if rules, err = self.overrideRules(rules); err != nil {
		return err
	}
	self.outputAndExit(self.checkSnapshots(ctx, datasets, rules))

	return nil
}

func (self *monitorSnapshots) overrideRules(
	rules []config.MonitorSnapshot,
) ([]config.MonitorSnapshot, error) {
	if self.prefix != "" {
		rules = []config.MonitorSnapshot{
			{
				Prefix:   self.prefix,
				Warning:  self.warning,
				Critical: self.critical,
			},
		}
	}

	if len(rules) == 0 {
		return nil, fmt.Errorf(
			"no monitor rules or cli args defined for job %q", self.job)
	}

	return rules, nil
}

func (self *monitorSnapshots) datasetsRules(
	ctx context.Context, jobConfig *config.JobEnum,
) (datasets []string, rules []config.MonitorSnapshot, err error) {
	var cfg config.MonitorSnapshots
	switch job := jobConfig.Ret.(type) {
	case *config.PushJob:
		cfg = job.MonitorSnapshots
		datasets, err = self.datasetsFromFilter(ctx, job.Filesystems)
	case *config.SnapJob:
		cfg = job.MonitorSnapshots
		datasets, err = self.datasetsFromFilter(ctx, job.Filesystems)
	case *config.SourceJob:
		cfg = job.MonitorSnapshots
		datasets, err = self.datasetsFromFilter(ctx, job.Filesystems)
	case *config.PullJob:
		cfg = job.MonitorSnapshots
		datasets, err = self.datasetsFromRootFs(ctx, job.RootFS, 0)
	case *config.SinkJob:
		cfg = job.MonitorSnapshots
		datasets, err = self.datasetsFromRootFs(ctx, job.RootFS, 1)
	default:
		err = fmt.Errorf("unknown job type %T", job)
	}

	if err == nil {
		if self.oldest {
			rules = cfg.Oldest
		} else {
			rules = cfg.Latest
		}
	}

	return
}

func (self *monitorSnapshots) datasetsFromFilter(
	ctx context.Context, ff config.FilesystemsFilter,
) ([]string, error) {
	filesystems, err := filters.DatasetMapFilterFromConfig(ff)
	if err != nil {
		return nil, fmt.Errorf("job %q has invalid filesystems: %w", self.job, err)
	}

	zfsProps, err := zfs.ZFSList(ctx, []string{"name"})
	if err != nil {
		return nil, err
	}

	filtered := make([]string, 0, len(zfsProps))
	for _, item := range zfsProps {
		path, err := zfs.NewDatasetPath(item[0])
		if err != nil {
			return nil, err
		}
		if ok, err := filesystems.Filter(path); err != nil {
			return nil, err
		} else if ok {
			filtered = append(filtered, item[0])
		}
	}

	return filtered, nil
}

func (self *monitorSnapshots) datasetsFromRootFs(
	ctx context.Context, rootFs string, skipN int,
) ([]string, error) {
	rootPath, err := zfs.NewDatasetPath(rootFs)
	if err != nil {
		return nil, err
	}

	zfsProps, err := zfs.ZFSList(ctx, []string{"name"}, "-r", rootFs)
	if err != nil {
		return nil, err
	}

	filtered := make([]string, 0, len(zfsProps))
	for _, item := range zfsProps {
		path, err := zfs.NewDatasetPath(item[0])
		if err != nil {
			return nil, err
		} else if path.Length() < rootPath.Length()+1+skipN {
			continue
		}
		if ph, err := zfs.ZFSGetFilesystemPlaceholderState(ctx, path); err != nil {
			return nil, err
		} else if ph.FSExists && !ph.IsPlaceholder {
			filtered = append(filtered, item[0])
		}
	}

	return filtered, nil
}

func (self *monitorSnapshots) checkSnapshots(
	ctx context.Context, datasets []string, rules []config.MonitorSnapshot,
) error {
	for _, dataset := range datasets {
		if err := self.checkDataset(ctx, dataset, rules); err != nil {
			return err
		}
	}

	return nil
}

func (self *monitorSnapshots) checkDataset(
	ctx context.Context, name string, rules []config.MonitorSnapshot,
) error {
	path, err := zfs.NewDatasetPath(name)
	if err != nil {
		return err
	}

	snaps, err := zfs.ZFSListFilesystemVersions(ctx, path,
		zfs.ListFilesystemVersionsOptions{Types: zfs.Snapshots})
	if err != nil {
		return err
	}

	latest := self.groupSnapshots(snaps, rules)
	for i, rule := range rules {
		const tooOldFmt = "%s %q too old: %q > %q"
		switch {
		case rule.Prefix == "" && latest[i].Creation.IsZero():
		case latest[i].Creation.IsZero():
			return newMonitorCriticalf(
				"%q has no snapshots with prefix %q", name, rule.Prefix)
		case time.Since(latest[i].Creation) >= rule.Critical:
			return newMonitorCriticalf(tooOldFmt, self.snapshotType(),
				latest[i].FullPath(name), time.Since(latest[i].Creation), rule.Critical)
		case rule.Warning > 0 && time.Since(latest[i].Creation) >= rule.Warning:
			return newMonitorWarningf(tooOldFmt, self.snapshotType(),
				latest[i].FullPath(name), time.Since(latest[i].Creation), rule.Warning)
		}
	}

	return nil
}

func (self *monitorSnapshots) groupSnapshots(
	snaps []zfs.FilesystemVersion, rules []config.MonitorSnapshot,
) []zfs.FilesystemVersion {
	latest := make([]zfs.FilesystemVersion, len(rules))
	unknownSnaps := snaps[:0]

	for i, rule := range rules {
		for _, snap := range snaps {
			if rule.Prefix == "" || strings.HasPrefix(snap.GetName(), rule.Prefix) {
				if latest[i].Creation.IsZero() || self.cmpSnapshots(snap, latest[i]) {
					latest[i] = snap
				}
			} else {
				unknownSnaps = append(unknownSnaps, snap)
			}
		}
		snaps = unknownSnaps
		unknownSnaps = snaps[:0]
		if len(snaps) == 0 {
			break
		}
	}
	return latest
}

func (self *monitorSnapshots) cmpSnapshots(
	new zfs.FilesystemVersion, old zfs.FilesystemVersion,
) bool {
	if self.oldest {
		return new.Creation.Before(old.Creation)
	}
	return new.Creation.After(old.Creation)
}

func (self *monitorSnapshots) outputAndExit(err error) {
	resp := monitoringplugin.NewResponse(
		fmt.Sprintf("job %q: %s snapshots", self.job, self.snapshotType()))

	if err != nil {
		status := fmt.Sprintf("job %q: %s", self.job, err)
		var checkResult monitorCheckResult
		if errors.As(err, &checkResult) {
			switch {
			case checkResult.critical:
				resp.UpdateStatus(monitoringplugin.CRITICAL, status)
			case checkResult.warning:
				resp.UpdateStatus(monitoringplugin.WARNING, status)
			default:
				resp.UpdateStatus(monitoringplugin.UNKNOWN, status)
			}
		} else {
			resp.UpdateStatus(monitoringplugin.UNKNOWN, status)
		}
	}

	resp.OutputAndExit()
}

func (self *monitorSnapshots) snapshotType() string {
	if self.oldest {
		return "oldest"
	}
	return "latest"
}

func newMonitorCriticalf(msg string, v ...interface{}) monitorCheckResult {
	return monitorCheckResult{
		msg:      fmt.Sprintf(msg, v...),
		critical: true,
	}
}

func newMonitorWarningf(msg string, v ...interface{}) monitorCheckResult {
	return monitorCheckResult{
		msg:     fmt.Sprintf(msg, v...),
		warning: true,
	}
}

type monitorCheckResult struct {
	msg      string
	critical bool
	warning  bool
}

func (self monitorCheckResult) Error() string {
	return self.msg
}

type monitorAlive struct{}

func (self *monitorAlive) run(
	ctx context.Context, subcmd *cli.Subcommand, args []string,
) error {
	resp := monitoringplugin.NewResponse("daemon alive")
	resp.SetOutputDelimiter("")
	defer resp.OutputAndExit()

	daemonVer, err := self.checkVersions(subcmd.Config().Global.Control.SockPath)
	if err != nil {
		self.updateErrStatus(err, resp)
	} else {
		resp.UpdateStatus(monitoringplugin.OK,
			fmt.Sprintf(", %s", daemonVer))
	}

	return nil
}

func (self *monitorAlive) checkVersions(sockPath string) (string, error) {
	clientVer := version.NewZreplVersionInformation().String()
	daemonVer, err := self.daemonVersion(sockPath)
	if err != nil {
		return "", err
	}

	if clientVer != daemonVer {
		return "", newMonitorWarningf("client version (%s) != daemon version (%s)",
			clientVer, daemonVer)
	}

	return daemonVer, nil
}

func (self *monitorAlive) daemonVersion(sockPath string) (string, error) {
	httpc, err := controlHttpClient(sockPath)
	if err != nil {
		return "", fmt.Errorf("failed http client for %q: %w", sockPath, err)
	}

	var ver version.ZreplVersionInformation
	err = jsonRequestResponse(httpc, daemon.ControlJobEndpointVersion, "", &ver)
	if err != nil {
		return "", newMonitorCriticalf("failed version request: %s", err)
	}

	return ver.String(), nil
}

func (self *monitorAlive) updateErrStatus(
	err error, resp *monitoringplugin.Response,
) {
	statusCode := monitoringplugin.UNKNOWN
	var checkResult monitorCheckResult
	if errors.As(err, &checkResult) {
		switch {
		case checkResult.critical:
			statusCode = monitoringplugin.CRITICAL
		case checkResult.warning:
			statusCode = monitoringplugin.WARNING
		}
	}
	resp.UpdateStatus(statusCode, err.Error())
}
