package main

import (
	"flag"
	"github.com/zrepl/zrepl/sshbytestream"
	"github.com/zrepl/zrepl/util"
	// "bytes"
	_ "bufio"
	// "strings"
	"io"
	"log"
	"os"
	_ "time"
)

func main() {

	mode := flag.String("mode", "", "incoming|outgoing")
	incomingFile := flag.String("incoming.file", "", "file to deliver to callers")
	outgoingHost := flag.String("outgoing.sshHost", "", "ssh host")
	outgoingUser := flag.String("outgoing.sshUser", "", "ssh user")
	outgoingIdentity := flag.String("outgoing.sshIdentity", "", "ssh private key")
	outgoingPort := flag.Uint("outgoing.sshPort", 22, "ssh port")
	outgoingFile := flag.String("outgoing.File", "", "")
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

		log.Printf("Chunk Count: %d\n", chunker.ChunkCount)

	case *mode == "outgoing":

		conn, err := sshbytestream.Outgoing(sshbytestream.SSHTransport{
			Host:         *outgoingHost,
			User:         *outgoingUser,
			IdentityFile: *outgoingIdentity,
			Port:         uint16(*outgoingPort),
		})
		if err != nil {
			panic(err)
		}

		f, err := os.OpenFile(*outgoingFile, os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}

		unchunker := chunking.NewUnchunker(conn)

		_, err = io.Copy(f, unchunker)
		if err != nil {
			panic(err)
		}

		conn.Close()

		log.Printf("Chunk Count: %d\n", unchunker.ChunkCount)

		os.Exit(0)

	default:
		panic("unsupported mode!")

	}

}
