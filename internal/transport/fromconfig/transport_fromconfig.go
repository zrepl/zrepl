// Package fromconfig instantiates transports based on zrepl config structures
// (see package config).
package fromconfig

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/LyingCak3/zrepl/internal/config"
	"github.com/LyingCak3/zrepl/internal/transport"
	"github.com/LyingCak3/zrepl/internal/transport/local"
	"github.com/LyingCak3/zrepl/internal/transport/ssh"
	"github.com/LyingCak3/zrepl/internal/transport/tcp"
	"github.com/LyingCak3/zrepl/internal/transport/tls"
)

func ListenerFactoryFromConfig(g *config.Global, in config.ServeEnum, parseFlags config.ParseFlags) (transport.AuthenticatedListenerFactory, error) {

	var (
		l   transport.AuthenticatedListenerFactory
		err error
	)
	switch v := in.Ret.(type) {
	case *config.TCPServe:
		l, err = tcp.TCPListenerFactoryFromConfig(g, v)
	case *config.TLSServe:
		l, err = tls.TLSListenerFactoryFromConfig(g, v, parseFlags)
	case *config.StdinserverServer:
		l, err = ssh.MultiStdinserverListenerFactoryFromConfig(g, v)
	case *config.LocalServe:
		l, err = local.LocalListenerFactoryFromConfig(g, v)
	default:
		return nil, errors.Errorf("internal error: unknown serve type %T", v)
	}

	return l, err
}

func ConnecterFromConfig(g *config.Global, in config.ConnectEnum, parseFlags config.ParseFlags) (transport.Connecter, error) {
	var (
		connecter transport.Connecter
		err       error
	)
	switch v := in.Ret.(type) {
	case *config.SSHStdinserverConnect:
		connecter, err = ssh.SSHStdinserverConnecterFromConfig(v)
	case *config.TCPConnect:
		connecter, err = tcp.TCPConnecterFromConfig(v)
	case *config.TLSConnect:
		connecter, err = tls.TLSConnecterFromConfig(v, parseFlags)
	case *config.LocalConnect:
		connecter, err = local.LocalConnecterFromConfig(v)
	default:
		panic(fmt.Sprintf("implementation error: unknown connecter type %T", v))
	}

	return connecter, err
}
