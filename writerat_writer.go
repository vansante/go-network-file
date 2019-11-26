package networkfile

import "io"

// WriterAtWriter makes a regular io.Writer out of an io.WriterAt.
// Not safe for concurrent use.
type WriterAtWriter struct {
	io.WriterAt

	offset int64
}

// Write writes to the io.WriterAt at the last offset + 1
func (w *WriterAtWriter) Write(buf []byte) (n int, err error) {
	n, err = w.WriteAt(buf, w.offset)
	w.offset += int64(n)
	return n, err
}
