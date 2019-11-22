package networkfile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	// HTTPTimeout is the time given for HTTP requests
	HTTPTimeout = 3 * time.Second
)

// file is the base file for the remote file handles
type file struct {
	embedLogger
	client       *http.Client
	baseURL      string
	sharedSecret string
	fileID       FileID
	offset       int64
}

// Seek seeks to the given offset from the given mode
func (f *file) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.offset = offset
	case io.SeekCurrent:
		f.offset += offset
	case io.SeekEnd:
		fi, err := f.stat()
		if err != nil {
			return 0, err
		}
		f.offset = fi.Size() + offset
	default:
		return 0, ErrUnsupportedOperation
	}
	return f.offset, nil
}

// FileID returns the fileID
func (f *file) FileID() FileID {
	return f.fileID
}

// SharedSecret returns the shared secret
func (f *file) SharedSecret() string {
	return f.sharedSecret
}

// Stat returns the remote file information
func (f *file) Stat() (fi os.FileInfo, err error) {
	info, err := f.stat()
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// Close tells the server to close the remote file
func (f *file) Close() (err error) {
	return f.close()
}

// prepareRequest prepares a new HTTP request
func (f *file) prepareRequest(method, url string, body io.Reader) (req *http.Request, err error) {
	req, err = http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set(HeaderSharedSecret, f.sharedSecret)
	return req, nil
}

// stat returns the remote file information
func (f *file) stat() (fi FileInfo, err error) {
	url := fmt.Sprintf("%s/%s", f.baseURL, f.fileID)
	req, err := f.prepareRequest(http.MethodOptions, url, nil)
	if err != nil {
		f.Errorf("file.stat: Error creating request: %v", err)
		return fi, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), HTTPTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := f.client.Do(req)
	if err != nil {
		f.Errorf("file.stat: Error executing request: %v", err)
		return fi, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	err = responseCodeToError(resp, http.StatusOK)
	if err != nil {
		f.Infof("file.stat: A remote error occurred: %v", err)
		return fi, err
	}

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&fi)
	if err != nil {
		f.Errorf("file.stat: Error decoding file info: %v", err)
		return fi, err
	}

	f.Debugf("file.stat: File info for %s: %v", f.fileID, fi)
	return fi, nil
}

// close tells the remote server to close the file
func (f *file) close() (err error) {
	url := fmt.Sprintf("%s/%s", f.baseURL, f.fileID)
	req, err := f.prepareRequest(http.MethodDelete, url, nil)
	if err != nil {
		f.Errorf("file.close: Error creating request: %v", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), HTTPTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := f.client.Do(req)
	if err != nil {
		f.Errorf("file.close: Error executing request: %v", err)
		return err
	}
	_ = resp.Body.Close()

	err = responseCodeToError(resp, http.StatusNoContent)
	if err != nil {
		f.Infof("file.close: A remote error occurred: %v", err)
		return err
	}

	f.Debugf("file.close: The remote file %s was closed", f.fileID)
	return nil
}
