package logic

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/replication/logic/pdu"
)

//go:generate enumer -type=InitialReplicationAutoResolution -trimprefix=InitialReplicationAutoResolution
type InitialReplicationAutoResolution uint32

const (
	InitialReplicationAutoResolutionMostRecent InitialReplicationAutoResolution = 1 << iota
	InitialReplicationAutoResolutionAll
	InitialReplicationAutoResolutionFail
)

var initialReplicationAutoResolutionConfigMap = map[InitialReplicationAutoResolution]string{
	InitialReplicationAutoResolutionMostRecent: "most_recent",
	InitialReplicationAutoResolutionAll:        "all",
	InitialReplicationAutoResolutionFail:       "fail",
}

func InitialReplicationAutoResolutionFromConfig(in string) (InitialReplicationAutoResolution, error) {
	for v, s := range initialReplicationAutoResolutionConfigMap {
		if s == in {
			return v, nil
		}
	}
	l := make([]string, 0, len(initialReplicationAutoResolutionConfigMap))
	for _, v := range InitialReplicationAutoResolutionValues() {
		l = append(l, initialReplicationAutoResolutionConfigMap[v])
	}
	return 0, fmt.Errorf("invalid value %q, must be one of %s", in, strings.Join(l, ", "))
}

type ConflictResolution struct {
	InitialReplication InitialReplicationAutoResolution
}

func (c *ConflictResolution) Validate() error {
	if !c.InitialReplication.IsAInitialReplicationAutoResolution() {
		return errors.Errorf("must be one of %s", InitialReplicationAutoResolutionValues())
	}
	return nil
}

func ConflictResolutionFromConfig(in *config.ConflictResolution) (*ConflictResolution, error) {

	initialReplication, err := InitialReplicationAutoResolutionFromConfig(in.InitialReplication)
	if err != nil {
		return nil, errors.Errorf("field `initial_replication` is invalid: %q is not one of %v", in.InitialReplication, InitialReplicationAutoResolutionValues())
	}

	return &ConflictResolution{
		InitialReplication: initialReplication,
	}, nil
}

type PlannerPolicy struct {
	ConflictResolution        *ConflictResolution    `validate:"required"`
	ReplicationConfig         *pdu.ReplicationConfig `validate:"required"`
	SizeEstimationConcurrency int                    `validate:"gte=1"`
}

var validate = validator.New()

func (p PlannerPolicy) Validate() error {
	if err := validate.Struct(p); err != nil {
		return err
	}
	if err := p.ConflictResolution.Validate(); err != nil {
		return err
	}
	return nil
}

func ReplicationConfigFromConfig(in *config.Replication) (*pdu.ReplicationConfig, error) {
	initial, err := pduReplicationGuaranteeKindFromConfig(in.Protection.Initial)
	if err != nil {
		return nil, errors.Wrap(err, "field 'initial'")
	}
	incremental, err := pduReplicationGuaranteeKindFromConfig(in.Protection.Incremental)
	if err != nil {
		return nil, errors.Wrap(err, "field 'incremental'")
	}
	return &pdu.ReplicationConfig{
		Protection: &pdu.ReplicationConfigProtection{
			Initial:     initial,
			Incremental: incremental,
		},
	}, nil
}

func pduReplicationGuaranteeKindFromConfig(in string) (k pdu.ReplicationGuaranteeKind, _ error) {
	switch in {
	case "guarantee_nothing":
		return pdu.ReplicationGuaranteeKind_GuaranteeNothing, nil
	case "guarantee_incremental":
		return pdu.ReplicationGuaranteeKind_GuaranteeIncrementalReplication, nil
	case "guarantee_resumability":
		return pdu.ReplicationGuaranteeKind_GuaranteeResumability, nil
	default:
		return k, errors.Errorf("%q is not in guarantee_{nothing,incremental,resumability}", in)
	}
}
