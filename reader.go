package networkfile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	// HTTPTimeout is the time given for HTTP requests
	HTTPTimeout = 3 * time.Second
)

type Reader struct {
	embedLogger
	client       *http.Client
	baseURL      string
	sharedSecret string
	fileID       FileID
	offset       int64
}

func NewReader(baseURL, sharedSecret string, fileID FileID) *Reader {
	return &Reader{
		client:       http.DefaultClient,
		baseURL:      baseURL,
		sharedSecret: sharedSecret,
		fileID:       fileID,
		offset:       0,
	}
}

func NewCustomClientReader(client *http.Client, baseURL, sharedSecret string, fileID FileID) *Reader {
	return &Reader{
		client:       client,
		baseURL:      baseURL,
		sharedSecret: sharedSecret,
		fileID:       fileID,
		offset:       0,
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

// Stat returns the remote file information
func (r *Reader) Stat() (fi os.FileInfo, err error) {
	info, err := r.stat()
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// prepareRequest prepares a new HTTP request
func (r *Reader) prepareRequest(method, url string) (req *http.Request, err error) {
	req, err = http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(SharedSecretHeader, r.sharedSecret)
	return req, nil
}

// stat returns the remote file information
func (r *Reader) stat() (fi FileInfo, err error) {
	url := fmt.Sprintf(
		"%s/%s/stat",
		r.baseURL,
		r.fileID,
	)

	req, err := r.prepareRequest(http.MethodGet, url)
	if err != nil {
		r.Errorf("Reader.stat: Error creating request: %v", err)
		return fi, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), HTTPTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := r.client.Do(req)
	if err != nil {
		r.Errorf("Reader.stat: Error executing request: %v", err)
		return fi, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	err = responseCodeToError(resp)
	if err != nil {
		r.Infof("Reader.stat: A remote error occurred: %v", err)
		return fi, err
	}

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&fi)
	if err != nil {
		r.Errorf("Reader.stat: Error decoding file info: %v", err)
		return fi, err
	}
	return fi, nil
}

func (r *Reader) read(buf []byte, offset int64) (n int, err error) {
	url := fmt.Sprintf(
		"%s/%s/read/%d/%d",
		r.baseURL,
		r.fileID,
		offset,
		len(buf),
	)

	req, err := r.prepareRequest(http.MethodGet, url)
	if err != nil {
		r.Errorf("Reader.read: Error creating request: %v", err)
		return 0, err
	}

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

	err = responseCodeToError(resp)
	if err != nil {
		r.Infof("Reader.read: A remote error occurred: %v", err)
		return 0, err
	}

	n, err = resp.Body.Read(buf)
	if err != nil {
		r.Errorf("Reader.read: Error reading http body: %v", err)
		return n, err
	}
	return n, nil
}
