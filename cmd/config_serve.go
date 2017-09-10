package cmd

import "github.com/pkg/errors"

type StdinserverListenerFactory struct {
	ClientIdentity string
}

func (StdinserverListenerFactory) Listen() AuthenticatedChannelListener {
	panic("implement me")
}

func parseStdinserverListenerFactory(i map[string]interface{}) (f *StdinserverListenerFactory, err error) {

	ci, ok := i["client_identity"]
	if !ok {
		err = errors.Errorf("must specify 'client_identity'")
		return
	}
	cs, ok := ci.(string)
	if !ok {
		err = errors.Errorf("must specify 'client_identity' as string, got %T", cs)
		return
	}
	f = &StdinserverListenerFactory{ClientIdentity: cs}
	return
}
