package networkfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
)

// file is the base file for the remote file handles
type file struct {
	client       *http.Client
	ctx          context.Context
	baseURL      string
	sharedSecret string
	fileID       FileID
	offset       int64
	logger       *slog.Logger
}

// SetLogger sets a new structured logger, replacing the default slog logger
func (f *file) SetLogger(logger *slog.Logger) {
	f.logger = logger
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
func (f *file) Stat() (os.FileInfo, error) {
	info, err := f.stat()
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// Close tells the server to close the remote file
func (f *file) Close() error {
	return f.close()
}

// prepareRequest prepares a new HTTP request
func (f *file) prepareRequest(method, url string, body io.Reader) (*http.Request, error) {
	var req *http.Request
	var err error
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
func (f *file) stat() (FileInfo, error) {
	fi := FileInfo{}

	url := fmt.Sprintf("%s/%s", f.baseURL, f.fileID)
	req, err := f.prepareRequest(http.MethodOptions, url, nil) // nolint:noctx
	if err != nil {
		f.logger.Error("networkfile.File.stat: Error creating request", "fileID", f.fileID, "error", err)
		return fi, err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			f.logger.Info("networkfile.File.stat: Context expired", "fileID", f.fileID, "error", err)
		} else {
			f.logger.Error("networkfile.File.stat: Error executing request", "fileID", f.fileID, "error", err)
		}
		return fi, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	err = responseCodeToError(resp, http.StatusOK)
	if err != nil {
		f.logger.Info("networkfile.File.stat: A remote error occurred", "fileID", f.fileID, "error", err)
		return fi, err
	}

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&fi)
	if err != nil {
		f.logger.Error("networkfile.File.stat: Error decoding file info", "fileID", f.fileID, "error", err)
		return fi, err
	}

	f.logger.Debug("networkfile.File.stat: File info", "fileID", f.fileID, "fileInfo", fi)
	return fi, nil
}

// close tells the remote server to close the file
func (f *file) close() error {
	url := fmt.Sprintf("%s/%s", f.baseURL, f.fileID)
	req, err := f.prepareRequest(http.MethodDelete, url, nil) //nolint: noctx
	if err != nil {
		f.logger.Error("networkfile.File.close: Error creating request", "error", err)
		return err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			f.logger.Info("networkfile.File.close: Context expired", "fileID", f.fileID, "error", err)
		} else {
			f.logger.Error("networkfile.File.close: Error executing request", "fileID", f.fileID, "error", err)
		}
		return err
	}
	_ = resp.Body.Close()

	err = responseCodeToError(resp, http.StatusNoContent)
	if err != nil {
		f.logger.Info("networkfile.File.close: A remote error occurred", "fileID", f.fileID, "error", err)
		return err
	}

	f.logger.Debug("networkfile.File.close: The remote file was closed", "fileID", f.fileID)
	return nil
}
