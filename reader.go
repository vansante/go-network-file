package networkfile

import (
	"context"
	"fmt"
	"net/http"
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
	return // TODO
}

// ReadAt reads from the remote file at a given offset
func (r *Reader) ReadAt(buf []byte, off int64) (n int, err error) {
	return // TODO
}

func (r *Reader) read(offset int64, buf []byte) (err error) {
	url := fmt.Sprintf(
		"%s/%s/%d/%d",
		r.baseURL,
		r.fileID,
		offset,
		len(buf),
	)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		r.Errorf("Reader.read: Error creating request: %v", err)
		return err
	}
	req.Header.Set(SharedSecretHeader, r.sharedSecret)

	ctx, cancel := context.WithTimeout(context.Background(), HTTPTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := r.client.Do(req)
	if err != nil {
		// TODO: Handle, check response code, etc
		return err
	}

	_, err = resp.Body.Read(buf)
	if err != nil {
		r.Errorf("Reader.read: Error reading http body: %v", err)
		return err
	}
	return nil
}
