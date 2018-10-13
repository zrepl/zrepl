package transport

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
	"unicode/utf8"
)

type HandshakeMessage struct {
	ProtocolVersion int
	Extensions []string
}

func (m *HandshakeMessage) Encode() ([]byte, error) {
	if m.ProtocolVersion <= 0 || m.ProtocolVersion > 9999 {
		return nil, fmt.Errorf("protocol version must be in [1, 9999]")
	}
	if len(m.Extensions) >= 9999 {
		return nil, fmt.Errorf("protocol only supports [0, 9999] extensions")
	}
	// EXTENSIONS is a count of subsequent \n separated lines that contain protocol extensions
	var extensions strings.Builder
	for i, ext := range m.Extensions {
		if strings.ContainsAny(ext, "\n") {
			return nil, fmt.Errorf("Extension #%d contains forbidden newline character", i)
		}
		if !utf8.ValidString(ext) {
			return nil, fmt.Errorf("Extension #%d is not valid UTF-8", i)
		}
		extensions.WriteString(ext)
		extensions.WriteString("\n")
	}
	withoutLen := fmt.Sprintf("ZREPL_ZFS_REPLICATION PROTOVERSION=%04d EXTENSIONS=%04d\n%s",
		m.ProtocolVersion, len(m.Extensions), extensions.String())
	withLen := fmt.Sprintf("%010d %s", len(withoutLen), withoutLen)
	return []byte(withLen), nil
}

func (m *HandshakeMessage) DecodeReader(r io.Reader, maxLen int) error {
	var lenAndSpace [11]byte
	if _, err := io.ReadFull(r, lenAndSpace[:]); err != nil {
		return err
	}
	if !utf8.Valid(lenAndSpace[:]) {
		return fmt.Errorf("invalid start of handshake message: not valid UTF-8")
	}
	var followLen int
	n, err := fmt.Sscanf(string(lenAndSpace[:]), "%010d ", &followLen)
	if n != 1 || err != nil {
		return fmt.Errorf("could not parse handshake message length")
	}
	if followLen > maxLen {
		return fmt.Errorf("handshake message length exceeds max length (%d vs %d)",
			followLen, maxLen)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, io.LimitReader(r, int64(followLen)))
	if err != nil {
		return err
	}

	var (
		protoVersion, extensionCount int
	)
	n, err = fmt.Fscanf(&buf, "ZREPL_ZFS_REPLICATION PROTOVERSION=%04d EXTENSIONS=%4d\n",
		&protoVersion, &extensionCount)
	if n != 2 || err != nil {
		return fmt.Errorf("could not parse handshake message: %s", err)
	}
	if protoVersion < 1 {
		return fmt.Errorf("invalid protocol version %q", protoVersion)
	}
	m.ProtocolVersion = protoVersion

	if extensionCount < 0 {
		return fmt.Errorf("invalid extension count %q", extensionCount)
	}
	if extensionCount == 0 {
		if buf.Len() != 0 {
			return fmt.Errorf("unexpected data trailing after header")
		}
		m.Extensions = nil
		return nil
	}
	s := buf.String()
	if strings.Count(s, "\n") != extensionCount {
		return fmt.Errorf("inconsistent extension count: found %d, header says %d", len(m.Extensions), extensionCount)
	}
	exts := strings.Split(s, "\n")
	if exts[len(exts)-1] != "" {
		return fmt.Errorf("unexpected data trailing after last extension newline")
	}
	m.Extensions = exts[0:len(exts)-1]

	return nil
}

func DoHandshakeCurrentVersion(conn net.Conn, deadline time.Time) error {
	// current protocol version is hardcoded here
	return DoHandshakeVersion(conn, deadline, 1)
}

func DoHandshakeVersion(conn net.Conn, deadline time.Time, version int) error {
	ours := HandshakeMessage{
		ProtocolVersion: version,
		Extensions: nil,
	}
	hsb, err := ours.Encode()
	if err != nil {
		return fmt.Errorf("could not encode protocol banner: %s", err)
	}

	conn.SetDeadline(deadline)
	_, err = io.Copy(conn, bytes.NewBuffer(hsb))
	if err != nil {
		return fmt.Errorf("could not send protocol banner: %s", err)
	}

	theirs := HandshakeMessage{}
	if err := theirs.DecodeReader(conn, 16 * 4096); err != nil { // FIXME constant
		return fmt.Errorf("could not decode protocol banner: %s", err)
	}

	if theirs.ProtocolVersion != ours.ProtocolVersion {
		return fmt.Errorf("protocol versions do not match: ours is %d, theirs is %d",
			ours.ProtocolVersion, theirs.ProtocolVersion)
	}
	// ignore extensions, we don't use them

	return nil
}
