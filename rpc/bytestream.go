package rpc

struct Unchunker {
	In 	io.Reader
	internalBuffer []byte
	remainingChunkSize uint
}

func NewUnchunker(conn io.Reader) {
	return Unchunker{In: conn} // TODO
}

func (c Unchunker) Read(b []byte) (n int, error) {
	// read min(c.internalBuffer.len, b.len) from c.internalBuffer into b
	// fill up internalBuffer
	//  1 read up to max(remainingChunkSize,internalBuffer.len) from In
	//  2 if remainingChunkSize == 0, read next chunk size, update remainingChunkSize
	//  3 goto 1
}

struct Chunker {
	In io.Reader
	MaxChunkSize uint

	ChunkBuf []byte // length of buf determines chunk size? --> would be fixed then
}