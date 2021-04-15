package logic

import (
	"github.com/go-playground/validator"
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/replication/logic/pdu"
)

type PlannerPolicy struct {
	EncryptedSend             tri // all sends must be encrypted (send -w, and encryption!=off)
	InitiallyAllSnapshots     bool
	ReplicationConfig         *pdu.ReplicationConfig
	SizeEstimationConcurrency int `validate:"gte=1"`
}

var validate = validator.New()

func (p PlannerPolicy) Validate() error {
	return validate.Struct(p)
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
