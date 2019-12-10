package networkfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// file is the base file for the remote file handles
type file struct {
	embedLogger
	client       *http.Client
	ctx          context.Context
	baseURL      string
	sharedSecret string
	fileID       FileID
	offset       int64
}

// SetContext sets a context on the requests executed
func (f *file) SetContext(ctx context.Context) {
	f.ctx = ctx
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
	if f.ctx == nil {
		req, err = http.NewRequest(method, url, body)
	} else {
		req, err = http.NewRequestWithContext(f.ctx, method, url, body)
	}
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
		f.Errorf("file.stat: Error creating request for %s: %v", f.fileID, err)
		return fi, err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			f.Infof("file.stat: Context timeout for %s: %v", f.fileID, err)
		} else {
			f.Errorf("file.stat: Error executing request for %s: %v", f.fileID, err)
		}
		return fi, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	err = responseCodeToError(resp, http.StatusOK)
	if err != nil {
		f.Infof("file.stat: A remote error occurred for %s: %v", f.fileID, err)
		return fi, err
	}

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&fi)
	if err != nil {
		f.Errorf("file.stat: Error decoding file info for %s: %v", f.fileID, err)
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

	resp, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			f.Infof("file.close: Context timeout for %s: %v", f.fileID, err)
		} else {
			f.Errorf("file.close: Error executing request for %s: %v", f.fileID, err)
		}
		return err
	}
	_ = resp.Body.Close()

	err = responseCodeToError(resp, http.StatusNoContent)
	if err != nil {
		f.Infof("file.close: A remote error occurred for %s: %v", f.fileID, err)
		return err
	}

	f.Debugf("file.close: The remote file %s was closed", f.fileID)
	return nil
}
