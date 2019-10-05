package networkfile

import (
	"io"
)

const (
	HttpCodeEOF           = 480
	HttpCodeUnexpectedEOF = 481
	HttpCodeShortBuffer   = 482
	HttpCodeShortWrite    = 483
	HttpCodeClosedPipe    = 484
	HttpCodeNoProgress    = 486
	HttpCodeUnknownError  = 490
)

var (
	httpCodeToErr = map[int]error{
		HttpCodeEOF:           io.EOF,
		HttpCodeUnexpectedEOF: io.ErrUnexpectedEOF,
		HttpCodeShortBuffer:   io.ErrShortBuffer,
		HttpCodeShortWrite:    io.ErrShortWrite,
		HttpCodeClosedPipe:    io.ErrClosedPipe,
		HttpCodeNoProgress:    io.ErrNoProgress,
	}

	errToHttpCode = map[error]int{
		io.EOF:              HttpCodeEOF,
		io.ErrUnexpectedEOF: HttpCodeUnexpectedEOF,
		io.ErrShortBuffer:   HttpCodeShortBuffer,
		io.ErrShortWrite:    HttpCodeShortWrite,
		io.ErrClosedPipe:    HttpCodeClosedPipe,
		io.ErrNoProgress:    HttpCodeNoProgress,
	}
)
