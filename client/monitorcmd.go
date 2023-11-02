package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/inexio/go-monitoringplugin"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/zfs"
)

var MonitorCmd = &cli.Subcommand{
	Use:   "monitor",
	Short: "Icinga/Nagios health checks",
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{newMonitorSnapshotsCmd()}
	},
}

func newMonitorSnapshotsCmd() *cli.Subcommand {
	runner := monitorSnapshots{}
	return &cli.Subcommand{
		Use:   "snapshots --job JOB",
		Short: "check snapshots are fresh according to rules",
		SetupFlags: func(f *pflag.FlagSet) {
			f.StringVarP(&runner.job, "job", "j", "", "the name of the job")
			f.StringVarP(&runner.prefix, "prefix", "p", "", "snapshot prefix")
			f.DurationVarP(&runner.critical, "crit", "c", 0, "critical snapshot age")
			f.DurationVarP(&runner.warning, "warn", "w", 0, "warning snapshot age")
		},
		SetupCobra: func(c *cobra.Command) {
			_ = c.MarkFlagRequired("job")
			c.MarkFlagsRequiredTogether("prefix", "crit")
		},
		Run: runner.Run,
	}
}

type monitorSnapshots struct {
	job      string
	prefix   string
	critical time.Duration
	warning  time.Duration
}

func (self *monitorSnapshots) Run(
	ctx context.Context, subcommand *cli.Subcommand, args []string,
) error {
	jobConfig, err := subcommand.Config().Job(self.job)
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
	switch job := jobConfig.Ret.(type) {
	case *config.PushJob:
		rules = job.MonitorSnapshots
		datasets, err = self.datasetsFromFilter(ctx, job.Filesystems)
	case *config.SnapJob:
		rules = job.MonitorSnapshots
		datasets, err = self.datasetsFromFilter(ctx, job.Filesystems)
	case *config.SourceJob:
		rules = job.MonitorSnapshots
		datasets, err = self.datasetsFromFilter(ctx, job.Filesystems)
	case *config.PullJob:
		rules = job.MonitorSnapshots
		datasets, err = self.datasetsFromRootFs(ctx, job.RootFS, 0)
	case *config.SinkJob:
		rules = job.MonitorSnapshots
		datasets, err = self.datasetsFromRootFs(ctx, job.RootFS, 1)
	default:
		err = fmt.Errorf("unknown job type %T", job)
	}

	if err != nil {
		rules = nil
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

	latest := self.latestSnapshots(snaps, rules)
	for i, rule := range rules {
		switch {
		case rule.Prefix == "" && latest[i].Creation.IsZero():
		case latest[i].Creation.IsZero():
			return newMonitorCriticalf(
				"%q has no snapshots with prefix %q", name, rule.Prefix)
		case time.Since(latest[i].Creation) >= rule.Critical:
			return newMonitorCriticalf("%q too old: %q > %q",
				latest[i].FullPath(name), time.Since(latest[i].Creation), rule.Critical)
		case rule.Warning > 0 && time.Since(latest[i].Creation) >= rule.Warning:
			return newMonitorWarningf("%q too old: %q > %q",
				latest[i].FullPath(name), time.Since(latest[i].Creation), rule.Warning)
		}
	}

	return nil
}

func (self *monitorSnapshots) latestSnapshots(
	snaps []zfs.FilesystemVersion, rules []config.MonitorSnapshot,
) []zfs.FilesystemVersion {
	latest := make([]zfs.FilesystemVersion, len(rules))
	unknownSnaps := snaps[:0]

	for i, rule := range rules {
		for _, snap := range snaps {
			if rule.Prefix == "" || strings.HasPrefix(snap.GetName(), rule.Prefix) {
				if latest[i].Creation.IsZero() || snap.Creation.After(latest[i].Creation) {
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

func (self *monitorSnapshots) outputAndExit(err error) {
	resp := monitoringplugin.NewResponse(
		fmt.Sprintf("job %q: all snapshots fresh", self.job))

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
