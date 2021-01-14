package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/kr/pretty"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/replication/logic"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/zfs"
	"github.com/zrepl/zrepl/zync/transport/sshdirect"
	"github.com/zrepl/zrepl/zync/transport/transportlistenerfromnetlistener"
)

var flagStdinserver = flag.String("stdinserver", "", "")
var flagStderrToFile = flag.String("stderrtofile", "", "")

const (
	modeSender   = "sender"
	modeReceiver = "receiver"
)

func connecter(ctx context.Context, u *url.URL, mode, fs string) (transport.Connecter, error) {
	switch u.Scheme {
	case "ssh":
		return connecterSSH(ctx, u, mode, fs)
	default:
		panic(fmt.Sprintf("unknown scheme %q", u.Scheme))
	}
}

func connecterSSH(ctx context.Context, u *url.URL, mode, fs string) (transport.Connecter, error) {
	var port uint16
	if u.Port() == "" {
		port = 22
	} else {
		portU64, err := strconv.ParseUint(u.Port(), 10, 16)
		if err != nil {
			return nil, errors.Wrap(err, "invalid port")
		}
		port = uint16(portU64)
	}

	fmt.Println(u.User)
	idFilePath, hasIdFile := u.User.Password()
	if hasIdFile {
		_, err := ioutil.ReadFile(idFilePath)
		if err != nil {
			fmt.Println(err)
			hasIdFile = false
		}
	}
	if !hasIdFile {
		return nil, errors.New("must set password to identity file path")
	}

	ep := sshdirect.Endpoint{
		Host:         u.Hostname(),
		Port:         port,
		User:         u.User.Username(),
		IdentityFile: idFilePath,
		RunCommand:   []string{"zync", "-stderrtofile", "/tmp/zync_server.log", "-stdinserver", mode, fs},
	}
	return sshdirect.NewConnecter(ctx, ep)
}

func onefsfilter(osname string) zfs.DatasetFilter {
	f, err := filters.DatasetMapFilterFromConfig(map[string]bool{
		osname: true,
	})
	if err != nil {
		panic(err)
	}
	return f
}

func makeEndpoint(mode, fs string) (interface{}, error) {
	switch mode {
	case modeSender:
		sc := endpoint.SenderConfig{
			FSF:     onefsfilter(fs),
			Encrypt: &zfs.NilBool{B: true},
			JobID:   endpoint.MustMakeJobID("sender"),
		}
		return endpoint.NewSender(sc), nil
	case modeReceiver:
		rpath, err := zfs.NewDatasetPath(fs)
		if err != nil {
			panic(err)
		}
		if err != nil {
			panic(err)
		}
		return endpoint.NewReceiver(endpoint.ReceiverConfig{
			JobID:                      endpoint.MustMakeJobID("receiver"),
			AppendClientIdentity:       false,
			RootWithoutClientComponent: rpath,
		}), nil
	default:
		return nil, fmt.Errorf("unknown mode %q", mode)
	}
}

func serve(ctx context.Context, handler rpc.Handler) error {

	// copy-pasta from passive.go
	ctxInterceptor := func(handlerCtx context.Context, info rpc.HandlerContextInterceptorData, handler func(ctx context.Context)) {
		// the handlerCtx is clean => need to inherit logging and tracing config from job context
		handlerCtx = logging.WithInherit(handlerCtx, ctx)
		handlerCtx = trace.WithInherit(handlerCtx, ctx)

		handlerCtx, endTask := trace.WithTaskAndSpan(handlerCtx, "handler", fmt.Sprintf("method=%q", info.FullMethod()))
		defer endTask()
		handler(handlerCtx)
	}

	l, err := sshdirect.ServeStdin()
	if err != nil {
		panic(err)
	}

	srv := rpc.NewServer(handler, rpc.GetLoggersOrPanic(ctx), ctxInterceptor)
	srv.Serve(ctx, transportlistenerfromnetlistener.WrapFixed(l, "fakeclientidentitymustnotbeempty"))
	return nil
}

func parseFSArg(ctx context.Context, mode, arg string) (interface{}, error) {
	u, err := url.Parse(arg)
	if err != nil {
		return nil, err
	}
	if !u.IsAbs() {
		return nil, fmt.Errorf("URL must be absolute, got %q", arg)
	}
	if u.Path[0] != '/' {
		panic("impl error: expecting leading /")
	}
	fs := u.Path[1:]
	switch u.Scheme {
	case "local":
		if u.Host != "" {
			panic("hostname must be empty for 'local' scheme")
		}
		return makeEndpoint(mode, fs)
	default:
		cn, err := connecter(ctx, u, mode, fs)
		if err != nil {
			return nil, err
		}
		return rpc.NewClient(cn, rpc.GetLoggersOrPanic(ctx)), nil
	}
}

// func parseEndpoints(s, r string) (sender, receiver logic.Endpoint, _ error) {
// 	sUrl, err := url.Parse(s)
// 	if err != nil {
// 		return nil, nil, err
// 	}
// 	rUrl, err := url.Parse((r))
// 	if err != nil {
// 		return nil, nil, err
// 	}
// 	if !sUrl.IsAbs() || !rUrl.IsAbs() {
// 		return nil, nil, fmt.Errorf("must have a scheme")
// 	}

// 	if sUrl.Scheme == "local" {&& rUrl.Scheme == "local" {
// 		var err error
// 		sender, err = makeEndpoint(modeSender, sUrl.Path)
// 		if err == nil {
// 			receiver, err = makeEndpoint(modeReceiver, rUrl.Path)
// 		}
// 		return sender, receiver, err
// 	} else {

// 	}
// }

func main() {

	cancelSigs := make(chan os.Signal)
	signal.Notify(cancelSigs, os.Interrupt, syscall.SIGTERM)

	ctx := context.Background()
	trace.WithTaskFromStackUpdateCtx(&ctx)

	ctx = logging.WithLoggers(ctx, logging.SubsystemLoggersWithUniversalLogger(logger.NewStderrDebugLogger()))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			select {
			case <-cancelSigs:
				cancel()
			}
		}
	}()

	flag.Parse()

	if *flagStderrToFile != "" {
		f, err := os.Create(*flagStderrToFile)
		if err != nil {
			panic(err)
		}
		syscall.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
		runtime.KeepAlive(f)
		// enough?
	}

	if *flagStdinserver != "" {
		if flag.NArg() != 1 {
			panic("usage: -stdinserver MODE FS")
		}
		h, err := makeEndpoint(*flagStdinserver, flag.Arg(0))
		if err != nil {
			panic(err)
		}
		err = serve(ctx, h.(rpc.Handler))
		if err != nil {
			panic(err)
		}
		return
	}

	if flag.NArg() != 2 {
		panic("usage: zync SENDER RECEIVER")
	}

	sender, err := parseFSArg(ctx, modeSender, flag.Arg(0))
	if err != nil {
		panic(err)
	}

	receiver, err := parseFSArg(ctx, modeReceiver, flag.Arg(1))
	if err != nil {
		panic(err)
	}

	pp := logic.PlannerPolicy{
		EncryptedSend: logic.TriFromBool(true), // FIXME add flag
		ReplicationConfig: pdu.ReplicationConfig{
			Protection: &pdu.ReplicationConfigProtection{
				Initial:     pdu.ReplicationGuaranteeKind_GuaranteeNothing,
				Incremental: pdu.ReplicationGuaranteeKind_GuaranteeNothing,
			},
		},
	}
	p := logic.NewPlanner(nil, nil, sender.(logic.Sender), receiver.(logic.Receiver), pp)
	_, wait := replication.Do(ctx, p)
	defer wait(true)

}

func local() {

	cancelSigs := make(chan os.Signal)
	signal.Notify(cancelSigs, os.Interrupt, syscall.SIGTERM)

	ctx := context.Background()
	trace.WithTaskFromStackUpdateCtx(&ctx)

	ctx = logging.WithLoggers(ctx, logging.SubsystemLoggersWithUniversalLogger(logger.NewStderrDebugLogger()))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			select {
			case <-cancelSigs:
				cancel()
			}
		}
	}()

	sc := endpoint.SenderConfig{
		FSF:     onefsfilter(os.Args[1]),
		Encrypt: &zfs.NilBool{B: true},
		JobID:   endpoint.MustMakeJobID("sender"),
	}
	s := endpoint.NewSender(sc)
	pretty.Println(s)
	rpath, err := zfs.NewDatasetPath(os.Args[2])
	if err != nil {
		panic(err)
	}
	r := endpoint.NewReceiver(endpoint.ReceiverConfig{
		JobID:                      endpoint.MustMakeJobID("receiver"),
		AppendClientIdentity:       false,
		RootWithoutClientComponent: rpath,
	})
	pretty.Println(r)
	pp := logic.PlannerPolicy{
		EncryptedSend: logic.TriFromBool(sc.Encrypt.B),
		ReplicationConfig: pdu.ReplicationConfig{
			Protection: &pdu.ReplicationConfigProtection{
				Initial:     pdu.ReplicationGuaranteeKind_GuaranteeNothing,
				Incremental: pdu.ReplicationGuaranteeKind_GuaranteeNothing,
			},
		},
	}
	p := logic.NewPlanner(nil, nil, s, r, pp)
	report, wait := replication.Do(ctx, p)
	defer wait(true)

	ticker := time.NewTicker(2 * time.Second)
	for !wait(false) {
		select {
		case <-ticker.C:
			// pretty.Println(report())
		}
	}
	pretty.Println(report())
}
