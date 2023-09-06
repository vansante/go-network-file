package networkfile

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	HTTPCodeEOF                  = 480
	HTTPCodeUnexpectedEOF        = 481
	HTTPCodeShortBuffer          = 482
	HTTPCodeShortWrite           = 483
	HTTPCodeClosedPipe           = 484
	HTTPCodeNoProgress           = 486
	HTTPCodeUnknownError         = 490
	HTTPCodeUnsupportedOperation = 491
)

var (
	ErrUnsupportedOperation = errors.New("unsupported operation")
	ErrUnauthorized         = errors.New("unauthorized: wrong secret key")
	ErrUnknownFile          = errors.New("not found: unknown file")

	HTTPCodeToErr = map[int]error{
		http.StatusUnauthorized:      ErrUnauthorized,
		http.StatusNotFound:          ErrUnknownFile,
		HTTPCodeEOF:                  io.EOF,
		HTTPCodeUnexpectedEOF:        io.ErrUnexpectedEOF,
		HTTPCodeShortBuffer:          io.ErrShortBuffer,
		HTTPCodeShortWrite:           io.ErrShortWrite,
		HTTPCodeClosedPipe:           io.ErrClosedPipe,
		HTTPCodeNoProgress:           io.ErrNoProgress,
		HTTPCodeUnsupportedOperation: ErrUnsupportedOperation,
	}

	errToHTTPCode = map[error]int{
		ErrUnauthorized:         http.StatusUnauthorized,
		ErrUnknownFile:          http.StatusNotFound,
		io.EOF:                  HTTPCodeEOF,
		io.ErrUnexpectedEOF:     HTTPCodeUnexpectedEOF,
		io.ErrShortBuffer:       HTTPCodeShortBuffer,
		io.ErrShortWrite:        HTTPCodeShortWrite,
		io.ErrClosedPipe:        HTTPCodeClosedPipe,
		io.ErrNoProgress:        HTTPCodeNoProgress,
		ErrUnsupportedOperation: HTTPCodeUnsupportedOperation,
	}
)

// writeErrorToResponseWriter writes the given error with appropriate status code to the HTTP writer.
func writeErrorToResponseWriter(rw http.ResponseWriter, err error) {
	code, ok := errToHTTPCode[err]
	if !ok {
		rw.WriteHeader(HTTPCodeUnknownError)
		_, _ = rw.Write([]byte(err.Error()))
		return
	}
	rw.WriteHeader(code)
}

// responseCodeToError returns the right error from the given response
func responseCodeToError(resp *http.Response, expected int) error {
	if resp.StatusCode == expected {
		return nil
	}

	err, ok := HTTPCodeToErr[resp.StatusCode]
	if ok {
		return err
	}

	buf := make([]byte, 512)
	n, err := resp.Body.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf(
			"%d: an unknown error occurred but the error could not be determined because: %w",
			resp.StatusCode, err,
		)
	}
	return fmt.Errorf("%d: an unknown error occurred: %s", resp.StatusCode, string(buf[:n]))
}
