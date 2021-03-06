package networkfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Writer is a byte writer for a remote io.Writer served by a FileServer
type Writer struct {
	file
}

// NewWriter creates a new remote Writer for the given URL, shared secret and FileID
func NewWriter(ctx context.Context, baseURL, sharedSecret string, fileID FileID) *Writer {
	return &Writer{
		file: file{
			client:       http.DefaultClient,
			ctx:          ctx,
			baseURL:      baseURL,
			sharedSecret: sharedSecret,
			fileID:       fileID,
			offset:       0,
		},
	}
}

// NewCustomClientWriter creates a new remote Writer for the given HTTP file, URL, shared secret and FileID
func NewCustomClientWriter(ctx context.Context, httpClient *http.Client, baseURL, sharedSecret string, fileID FileID) *Writer {
	return &Writer{
		file: file{
			client:       httpClient,
			ctx:          ctx,
			baseURL:      baseURL,
			sharedSecret: sharedSecret,
			fileID:       fileID,
			offset:       0,
		},
	}
}

// PutURL returns the URL at which the file can be PUT in a single request
func (w *Writer) PutURL() string {
	return fmt.Sprintf("%s/%s?%s=%s", w.baseURL, w.fileID, GETSharedSecret, w.sharedSecret)
}

// Write writes to the remote file
func (w *Writer) Write(buf []byte) (n int, err error) {
	n, err = w.write(buf, w.offset)
	w.offset += int64(n)
	return n, err
}

// WriteAt writes to the remote file at a given offset
func (w *Writer) WriteAt(buf []byte, offset int64) (n int, err error) {
	return w.write(buf, offset)
}

func (w *Writer) write(buf []byte, offset int64) (n int, err error) {
	url := fmt.Sprintf("%s/%s", w.baseURL, w.fileID)

	req, err := w.prepareRequest(http.MethodPatch, url, bytes.NewReader(buf))
	if err != nil {
		w.Errorf("Writer.write: Error creating request for %s: %v", w.fileID, err)
		return 0, err
	}
	req.Header.Set(HeaderRange, fmt.Sprintf("%d-%d", offset, len(buf)))

	resp, err := w.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			w.Infof("Writer.write: Context expired for %s: %v", w.fileID, err)
		} else {
			w.Errorf("Writer.write: Error executing request for %s: %v", w.fileID, err)
		}
		return 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	err = responseCodeToError(resp, http.StatusNoContent)
	if err != nil {
		w.Infof("Writer.write: A remote error occurred for %s: %v", w.fileID, err)
		return 0, err
	}

	var servOffset, servLength int64
	matches, err := fmt.Sscanf(resp.Header.Get(HeaderRange), "%d-%d", &servOffset, &servLength)
	if err != nil || matches != 2 {
		w.Errorf("Writer.write: Error parsing range header (%s) for %s: %v", resp.Header.Get(HeaderRange), w.fileID, err)
		return 0, err
	}

	if servOffset != offset {
		w.Errorf("Writer.write: Server returned unexpected offset (%d != %d) for %s", offset, servOffset, w.fileID)
		return 0, errors.New("unexpected server offset")
	}

	if servLength != int64(len(buf)) {
		w.Errorf("Writer.write: Server returned unexpected length (%d != %d) for %s", len(buf), servLength, w.fileID)
		return 0, errors.New("unexpected server length")
	}

	w.Debugf("Writer.write: Wrote %d bytes from offset %d for %s", len(buf), offset, w.fileID)
	return int(servLength), nil
}
