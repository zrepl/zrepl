package main

import (
	"flag"
	"github.com/zrepl/zrepl/model"
	"github.com/zrepl/zrepl/sshbytestream"
	"github.com/zrepl/zrepl/util"
	// "bytes"
	_ "bufio"
	// "strings"
	"fmt"
	"io"
	"os"
	_ "time"
)

func main() {

	mode := flag.String("mode", "", "incoming|outgoing")
	incomingFile := flag.String("incoming.file", "", "file to deliver to callers")
	outgoingHost := flag.String("outgoing.sshHost", "", "ssh host")
	outgoingUser := flag.String("outgoing.sshUser", "", "ssh user")
	outgoingPort := flag.Uint("outgoing.sshPort", 22, "ssh port")
	flag.Parse()

	switch {
	case (*mode == "incoming"):

		conn, err := sshbytestream.Incoming()
		if err != nil {
			panic(err)
		}

		file, err := os.Open(*incomingFile)
		if err != nil {
			panic(err)
		}

		chunker := chunking.NewChunker(file)

		_, err = io.Copy(conn, &chunker)
		if err != nil && err != io.EOF {
			panic(err)
		}

		fmt.Fprintf(os.Stderr, "Chunk Count: %d\n", chunker.ChunkCount)

	case *mode == "outgoing":

		conn, err := sshbytestream.Outgoing("client", model.SSHTransport{
			Host:                 *outgoingHost,
			User:                 *outgoingUser,
			Port:                 uint16(*outgoingPort),
			Options:              []string{"Compression=no"},
			TransportOpenCommand: []string{"/tmp/sshwrap", "-mode", "incoming", "-incoming.file", "/random.img"},
		})

		if err != nil {
			panic(err)
		}

		unchunker := chunking.NewUnchunker(conn)

		_, err = io.Copy(os.Stdout, &unchunker)
		if err != nil {
			panic(err)
		}

		conn.Close()

		fmt.Fprintf(os.Stderr, "Chunk Count: %d\n", unchunker.ChunkCount)

		os.Exit(0)

	default:
		panic("unsupported mode!")

	}

}
