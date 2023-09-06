package networkfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// Reader is a byte reader for a remote io.Reader served by a FileServer
type Reader struct {
	file
}

// NewReader creates a new remote Reader for the given URL, shared secret and FileID
func NewReader(ctx context.Context, baseURL, sharedSecret string, fileID FileID) *Reader {
	return &Reader{
		file: file{
			client:       http.DefaultClient,
			ctx:          ctx,
			baseURL:      baseURL,
			sharedSecret: sharedSecret,
			fileID:       fileID,
			offset:       0,
			logger:       slog.Default(),
		},
	}
}

// NewCustomClientReader creates a new remote Reader for the given HTTP file, URL, shared secret and FileID
func NewCustomClientReader(ctx context.Context, httpClient *http.Client, baseURL, sharedSecret string, fileID FileID) *Reader {
	return &Reader{
		file: file{
			client:       httpClient,
			ctx:          ctx,
			baseURL:      baseURL,
			sharedSecret: sharedSecret,
			fileID:       fileID,
			offset:       0,
			logger:       slog.Default(),
		},
	}
}

// FullReadURL returns the URL at which the file can be downloaded completely via a normal GET request without this reader
func (r *Reader) FullReadURL() string {
	return fmt.Sprintf("%s/%s?%s=%s", r.baseURL, r.fileID, GETSharedSecret, r.sharedSecret)
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

	req, err := r.prepareRequest(http.MethodGet, url, nil) // nolint:noctx
	if err != nil {
		r.logger.Error("networkfile.Reader.read: Error creating request", "fileID", r.fileID, "error", err)
		return 0, err
	}
	req.Header.Set(HeaderRange, fmt.Sprintf("%d-%d", offset, len(buf)))

	resp, err := r.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			r.logger.Info("networkfile.Reader.read: Context expired", "fileID", r.fileID, "error", err)
		} else {
			r.logger.Error("networkfile.Reader.read: Error executing request", "fileID", r.fileID, "error", err)
		}
		return 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	err = responseCodeToError(resp, http.StatusPartialContent)
	if err != nil {
		r.logger.Info("networkfile.Reader.read: A remote error occurred", "fileID", r.fileID, "error", err)
		return 0, err
	}

	for len(buf) > 0 && err == nil {
		var copied int
		copied, err = resp.Body.Read(buf)
		n += copied
		buf = buf[copied:]
	}

	if err != nil && !errors.Is(err, io.EOF) {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			r.logger.Info("networkfile.Reader.read: Context expired", "fileID", r.fileID, "error", err)
		} else {
			r.logger.Error("networkfile.Reader.read: Error reading http body", "fileID", r.fileID, "error", err)
		}
		return n, err
	}
	if n == 0 {
		return n, io.EOF
	}
	return n, nil
}
