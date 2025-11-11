package endpoint

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	"github.com/LyingCak3/zrepl/internal/replication/logic/pdu"
	"github.com/LyingCak3/zrepl/internal/zfs"
)

type ReplicationGuaranteeOptions struct {
	Initial     ReplicationGuaranteeKind
	Incremental ReplicationGuaranteeKind
}

func replicationGuaranteeOptionsFromPDU(in *pdu.ReplicationConfigProtection) (o ReplicationGuaranteeOptions, _ error) {
	if in == nil {
		return o, errors.New("pdu.ReplicationConfigProtection must not be nil")
	}
	initial, err := replicationGuaranteeKindFromPDU(in.GetInitial())
	if err != nil {
		return o, errors.Wrap(err, "pdu.ReplicationConfigProtection: field Initial")
	}
	incremental, err := replicationGuaranteeKindFromPDU(in.GetIncremental())
	if err != nil {
		return o, errors.Wrap(err, "pdu.ReplicationConfigProtection: field Incremental")
	}
	o = ReplicationGuaranteeOptions{
		Initial:     initial,
		Incremental: incremental,
	}
	return o, nil
}

func replicationGuaranteeKindFromPDU(in pdu.ReplicationGuaranteeKind) (k ReplicationGuaranteeKind, _ error) {
	switch in {
	case pdu.ReplicationGuaranteeKind_GuaranteeNothing:
		return ReplicationGuaranteeKindNone, nil
	case pdu.ReplicationGuaranteeKind_GuaranteeIncrementalReplication:
		return ReplicationGuaranteeKindIncremental, nil
	case pdu.ReplicationGuaranteeKind_GuaranteeResumability:
		return ReplicationGuaranteeKindResumability, nil

	case pdu.ReplicationGuaranteeKind_GuaranteeInvalid:
		fallthrough
	default:
		return k, errors.Errorf("%q", in.String())
	}
}

func (o ReplicationGuaranteeOptions) Strategy(incremental bool) ReplicationGuaranteeStrategy {
	g := o.Initial
	if incremental {
		g = o.Incremental
	}
	return ReplicationGuaranteeFromKind(g)
}

//go:generate enumer -type=ReplicationGuaranteeKind -json -transform=snake -trimprefix=ReplicationGuaranteeKind
type ReplicationGuaranteeKind int

const (
	ReplicationGuaranteeKindResumability ReplicationGuaranteeKind = 1 << iota
	ReplicationGuaranteeKindIncremental
	ReplicationGuaranteeKindNone
)

type ReplicationGuaranteeStrategy interface {
	Kind() ReplicationGuaranteeKind
	SenderPreSend(ctx context.Context, jid JobID, sendArgs *zfs.ZFSSendArgsValidated) (keep []Abstraction, err error)
	ReceiverPostRecv(ctx context.Context, jid JobID, fs string, toRecvd zfs.FilesystemVersion) (keep []Abstraction, err error)
	SenderPostRecvConfirmed(ctx context.Context, jid JobID, fs string, to zfs.FilesystemVersion) (keep []Abstraction, err error)
}

func ReplicationGuaranteeFromKind(k ReplicationGuaranteeKind) ReplicationGuaranteeStrategy {
	switch k {
	case ReplicationGuaranteeKindNone:
		return ReplicationGuaranteeNone{}
	case ReplicationGuaranteeKindIncremental:
		return ReplicationGuaranteeIncremental{}
	case ReplicationGuaranteeKindResumability:
		return ReplicationGuaranteeResumability{}
	default:
		panic(fmt.Sprintf("unreachable: %q %T", k, k))
	}
}

type ReplicationGuaranteeNone struct{}

func (g ReplicationGuaranteeNone) String() string { return "none" }

func (g ReplicationGuaranteeNone) Kind() ReplicationGuaranteeKind {
	return ReplicationGuaranteeKindNone
}

func (g ReplicationGuaranteeNone) SenderPreSend(ctx context.Context, jid JobID, sendArgs *zfs.ZFSSendArgsValidated) (keep []Abstraction, err error) {
	return nil, nil
}

func (g ReplicationGuaranteeNone) ReceiverPostRecv(ctx context.Context, jid JobID, fs string, toRecvd zfs.FilesystemVersion) (keep []Abstraction, err error) {
	return nil, nil
}

func (g ReplicationGuaranteeNone) SenderPostRecvConfirmed(ctx context.Context, jid JobID, fs string, to zfs.FilesystemVersion) (keep []Abstraction, err error) {
	return nil, nil
}

type ReplicationGuaranteeIncremental struct{}

func (g ReplicationGuaranteeIncremental) String() string { return "incremental" }

func (g ReplicationGuaranteeIncremental) Kind() ReplicationGuaranteeKind {
	return ReplicationGuaranteeKindIncremental
}

func (g ReplicationGuaranteeIncremental) SenderPreSend(ctx context.Context, jid JobID, sendArgs *zfs.ZFSSendArgsValidated) (keep []Abstraction, err error) {
	if sendArgs.FromVersion != nil {
		from, err := CreateTentativeReplicationCursor(ctx, sendArgs.FS, *sendArgs.FromVersion, jid)
		if err != nil {
			if err == zfs.ErrBookmarkCloningNotSupported {
				getLogger(ctx).WithField("replication_guarantee", g).
					WithField("bookmark", sendArgs.From.FullPath(sendArgs.FS)).
					Info("bookmark cloning is not supported, speculating that `from` will not be destroyed until step is done")
			} else {
				return nil, err
			}
		}
		keep = append(keep, from)
	}
	to, err := CreateTentativeReplicationCursor(ctx, sendArgs.FS, sendArgs.ToVersion, jid)
	if err != nil {
		return nil, err
	}
	keep = append(keep, to)

	return keep, nil
}

func (g ReplicationGuaranteeIncremental) ReceiverPostRecv(ctx context.Context, jid JobID, fs string, toRecvd zfs.FilesystemVersion) (keep []Abstraction, err error) {
	return receiverPostRecvCommon(ctx, jid, fs, toRecvd)
}

func (g ReplicationGuaranteeIncremental) SenderPostRecvConfirmed(ctx context.Context, jid JobID, fs string, to zfs.FilesystemVersion) (keep []Abstraction, err error) {
	return senderPostRecvConfirmedCommon(ctx, jid, fs, to)
}

type ReplicationGuaranteeResumability struct{}

func (g ReplicationGuaranteeResumability) String() string { return "resumability" }

func (g ReplicationGuaranteeResumability) Kind() ReplicationGuaranteeKind {
	return ReplicationGuaranteeKindResumability
}

func (g ReplicationGuaranteeResumability) SenderPreSend(ctx context.Context, jid JobID, sendArgs *zfs.ZFSSendArgsValidated) (keep []Abstraction, err error) {
	// try to hold the FromVersion
	if sendArgs.FromVersion != nil {
		if sendArgs.FromVersion.Type == zfs.Bookmark {
			getLogger(ctx).WithField("replication_guarantee", g).WithField("fromVersion", sendArgs.FromVersion.FullPath(sendArgs.FS)).
				Debug("cannot hold a bookmark, speculating that `from` will not be destroyed until step is done")
		} else {
			from, err := HoldStep(ctx, sendArgs.FS, *sendArgs.FromVersion, jid)
			if err != nil {
				return nil, err
			}
			keep = append(keep, from)
		}
		// fallthrough
	}

	to, err := HoldStep(ctx, sendArgs.FS, sendArgs.ToVersion, jid)
	if err != nil {
		return nil, err
	}
	keep = append(keep, to)

	return keep, nil
}

func (g ReplicationGuaranteeResumability) ReceiverPostRecv(ctx context.Context, jid JobID, fs string, toRecvd zfs.FilesystemVersion) (keep []Abstraction, err error) {
	return receiverPostRecvCommon(ctx, jid, fs, toRecvd)
}

func (g ReplicationGuaranteeResumability) SenderPostRecvConfirmed(ctx context.Context, jid JobID, fs string, to zfs.FilesystemVersion) (keep []Abstraction, err error) {
	return senderPostRecvConfirmedCommon(ctx, jid, fs, to)
}

// helper function used by multiple strategies
func senderPostRecvConfirmedCommon(ctx context.Context, jid JobID, fs string, to zfs.FilesystemVersion) (keep []Abstraction, err error) {

	log := getLogger(ctx).WithField("toVersion", to.FullPath(fs))

	toReplicationCursor, err := CreateReplicationCursor(ctx, fs, to, jid)
	if err != nil {
		if err == zfs.ErrBookmarkCloningNotSupported {
			log.Debug("not setting replication cursor, bookmark cloning not supported")
		} else {
			msg := "cannot move replication cursor, keeping hold on `to` until successful"
			log.WithError(err).Error(msg)
			err = errors.Wrap(err, msg)
			return nil, err
		}
	} else {
		log.WithField("to_cursor", toReplicationCursor.String()).Info("successfully created `to` replication cursor")
	}

	return []Abstraction{toReplicationCursor}, nil
}

// helper function used by multiple strategies
func receiverPostRecvCommon(ctx context.Context, jid JobID, fs string, toRecvd zfs.FilesystemVersion) (keep []Abstraction, err error) {
	getLogger(ctx).Debug("create new last-received-hold")
	lrh, err := CreateLastReceivedHold(ctx, fs, toRecvd, jid)
	if err != nil {
		return nil, err
	}
	return []Abstraction{lrh}, nil
}
