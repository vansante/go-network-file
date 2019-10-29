package networkfile

import "io"

// ReaderAtReader makes a regular io.Reader out of an io.ReaderAt.
// Not safe for concurrent use.
type ReaderAtReader struct {
	io.ReaderAt

	offset int64
}

// Read reads from the io.ReaderAt at the last offset + 1
func (r *ReaderAtReader) Read(buf []byte) (n int, err error) {
	n, err = r.ReadAt(buf, r.offset)
	r.offset += int64(n)
	return n, err
}
