package networkfile

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	HttpCodeEOF                  = 480
	HttpCodeUnexpectedEOF        = 481
	HttpCodeShortBuffer          = 482
	HttpCodeShortWrite           = 483
	HttpCodeClosedPipe           = 484
	HttpCodeNoProgress           = 486
	HttpCodeUnknownError         = 490
	HttpCodeUnsupportedOperation = 491
)

var (
	ErrUnsupportedOperation = errors.New("unsupported operation")
	ErrUnauthorized         = errors.New("unauthorized: wrong secret key")
	ErrUnknownFile          = errors.New("not found: unknown file")

	httpCodeToErr = map[int]error{
		http.StatusUnauthorized:      ErrUnauthorized,
		http.StatusNotFound:          ErrUnknownFile,
		HttpCodeEOF:                  io.EOF,
		HttpCodeUnexpectedEOF:        io.ErrUnexpectedEOF,
		HttpCodeShortBuffer:          io.ErrShortBuffer,
		HttpCodeShortWrite:           io.ErrShortWrite,
		HttpCodeClosedPipe:           io.ErrClosedPipe,
		HttpCodeNoProgress:           io.ErrNoProgress,
		HttpCodeUnsupportedOperation: ErrUnsupportedOperation,
	}

	errToHttpCode = map[error]int{
		ErrUnauthorized:         http.StatusUnauthorized,
		ErrUnknownFile:          http.StatusNotFound,
		io.EOF:                  HttpCodeEOF,
		io.ErrUnexpectedEOF:     HttpCodeUnexpectedEOF,
		io.ErrShortBuffer:       HttpCodeShortBuffer,
		io.ErrShortWrite:        HttpCodeShortWrite,
		io.ErrClosedPipe:        HttpCodeClosedPipe,
		io.ErrNoProgress:        HttpCodeNoProgress,
		ErrUnsupportedOperation: HttpCodeUnsupportedOperation,
	}
)

// writeErrorToResponseWriter writes the given error with appropriate status code to the http writer.
func writeErrorToResponseWriter(rw http.ResponseWriter, err error) {
	code, ok := errToHttpCode[err]
	if !ok {
		rw.WriteHeader(HttpCodeUnknownError)
		_, _ = rw.Write([]byte(err.Error()))
		return
	}
	rw.WriteHeader(code)
}

// responseCodeToError returns the right error from the given response
func responseCodeToError(resp *http.Response) (err error) {
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	err, ok := httpCodeToErr[resp.StatusCode]
	if ok {
		return err
	}

	buf := make([]byte, 512)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		return fmt.Errorf(
			"%d: an unknown error occurred but the error could not be determined because: %v",
			resp.StatusCode, err,
		)
	}
	return fmt.Errorf("%d: an unknown error occurred: %s", resp.StatusCode, string(buf[:n]))
}
