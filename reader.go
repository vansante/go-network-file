package networkfile

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Reader is a byte reader for a remote io.Reader served by a FileServer
type Reader struct {
	file
}

// NewReader creates a new remote Reader for the given URL, shared secret and FileID
func NewReader(baseURL, sharedSecret string, fileID FileID) *Reader {
	return &Reader{
		file: file{
			client:       http.DefaultClient,
			baseURL:      baseURL,
			sharedSecret: sharedSecret,
			fileID:       fileID,
			offset:       0,
		},
	}
}

// NewCustomClientReader creates a new remote Reader for the given HTTP file, URL, shared secret and FileID
func NewCustomClientReader(httpClient *http.Client, baseURL, sharedSecret string, fileID FileID) *Reader {
	return &Reader{
		file: file{
			client:       httpClient,
			baseURL:      baseURL,
			sharedSecret: sharedSecret,
			fileID:       fileID,
			offset:       0,
		},
	}
}

// Read reads from the remote file
func (r *Reader) Read(buf []byte) (n int, err error) {
	n, err = r.read(buf, r.offset)
	r.offset += int64(n)
	return n, err
}

// ReadAt reads from the remote file at a given offset
func (r *Reader) ReadAt(buf []byte, offset int64) (n int, err error) {
	return r.read(buf, offset)
}

func (r *Reader) read(buf []byte, offset int64) (n int, err error) {
	url := fmt.Sprintf("%s/%s", r.baseURL, r.fileID)

	req, err := r.prepareRequest(http.MethodGet, url, nil)
	if err != nil {
		r.Errorf("Reader.read: Error creating request: %v", err)
		return 0, err
	}
	req.Header.Set(HeaderRange, fmt.Sprintf("%d-%d", offset, len(buf)))

	ctx, cancel := context.WithTimeout(context.Background(), HTTPTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := r.client.Do(req)
	if err != nil {
		r.Errorf("Reader.read: Error executing request: %v", err)
		return 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	err = responseCodeToError(resp, http.StatusPartialContent)
	if err != nil {
		r.Infof("Reader.read: A remote error occurred: %v", err)
		return 0, err
	}

	n, err = resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		r.Errorf("Reader.read: Error reading http body: %v", err)
		return n, err
	}

	eof := resp.Header.Get(HeaderIsEOF) == "true"

	r.Debugf("Reader.read: Read %d bytes from offset %d in file %s [EOF: %v]", n, offset, r.fileID, eof)
	if eof {
		return n, io.EOF
	}
	return n, nil
}
