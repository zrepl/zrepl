package connecter

import (
	"fmt"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/config"
)

func FromConfig(g config.Global, in config.ConnectEnum) (streamrpc.Connecter, error) {
	switch v := in.Ret.(type) {
	case *config.SSHStdinserverConnect:
		return SSHStdinserverConnecterFromConfig(v)
	case *config.TCPConnect:
		return TCPConnecterFromConfig(v)
	case *config.TLSConnect:
		return TLSConnecterFromConfig(v)
	default:
		panic(fmt.Sprintf("implementation error: unknown connecter type %T", v))
	}
}
