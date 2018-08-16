package replication

type Report struct {
	Status    string
	Problem   string
	Completed []*FilesystemReplicationReport
	Pending   []*FilesystemReplicationReport
	Active    *FilesystemReplicationReport
}

type StepReport struct {
	From, To string
	Status   string
	Problem  string
}

type FilesystemReplicationReport struct {
	Filesystem string
	Status     string
	Problem    string
	Steps      []*StepReport
}

func stepReportFromStep(step *FSReplicationStep) *StepReport {
	var from string // FIXME follow same convention as ZFS: to should be nil on full send
	if step.from != nil {
		from = step.from.RelName()
	}
	rep := StepReport{
		From:   from,
		To:     step.to.RelName(),
		Status: step.state.String(),
	}
	return &rep
}

// access to fsr's members must be exclusive
func filesystemReplicationReport(fsr *FSReplication) *FilesystemReplicationReport {
	fsr.lock.Lock()
	defer fsr.lock.Unlock()

	rep := FilesystemReplicationReport{
		Filesystem: fsr.fs.Path,
		Status:     fsr.state.String(),
	}

	if fsr.state&FSPermanentError != 0 {
		rep.Problem = fsr.err.Error()
		return &rep
	}

	rep.Steps = make([]*StepReport, 0, len(fsr.completed)+len(fsr.pending) + 1)
	for _, step := range fsr.completed {
		rep.Steps = append(rep.Steps, stepReportFromStep(step))
	}
	if fsr.current != nil {
		rep.Steps = append(rep.Steps, stepReportFromStep(fsr.current))
	}
	for _, step := range fsr.pending {
		rep.Steps = append(rep.Steps, stepReportFromStep(step))
	}
	return &rep
}

func (r *Replication) Report() *Report {
	r.lock.Lock()
	defer r.lock.Unlock()

	rep := Report{
		Status: r.state.String(),
	}

	if r.state&(Planning|PlanningError|ContextDone) != 0 {
		switch r.state {
		case PlanningError:
			rep.Problem = r.planningError.Error()
		case ContextDone:
			rep.Problem = r.contextError.Error()
		}
		return &rep
	}

	rep.Pending = make([]*FilesystemReplicationReport, 0, r.queue.Len())
	rep.Completed = make([]*FilesystemReplicationReport, 0, len(r.completed)) // room for active (potentially)

	r.queue.Foreach(func (h *replicationQueueItemHandle){
		rep.Pending = append(rep.Pending, filesystemReplicationReport(h.GetFSReplication()))
	})
	for _, fsr := range r.completed {
		rep.Completed = append(rep.Completed,  filesystemReplicationReport(fsr))
	}

	return &rep
}
