package networkfile

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrFileIDTaken = errors.New("fileID is already being used")

const (
	// MinumumBufferSize is the minimum buffer size for one HTTP call
	MinumumBufferSize = 1

	// HeaderSharedSecret is the name of the header where the shared secret is passed
	HeaderSharedSecret = "X-SharedSecret"

	// GETSharedSecret is the name of the GET parameter that is also an acceptable way of transferring the shared secret
	GETSharedSecret = "shared-secret"

	// HeaderRange is the header used for sending ranges
	HeaderRange = "X-Range"

	// HeaderContentLength is the header used for sending the file size
	HeaderContentLength = "Content-Length"
)

// RandomSharedSecret returns a random shared secret
func RandomSharedSecret(bytes int) (string, error) {
	buf := make([]byte, bytes)
	_, err := rand.Read(buf)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// FileServer is an HTTP server that serves files as io.Readers or io.Writers
type FileServer struct {
	urlPrefix         string
	sharedSecret      string
	readers           map[FileID]*concurrentReadSeeker
	writers           map[FileID]*concurrentWriteSeeker
	allowStat         bool // Allow disclosing the information of Stat()
	allowClose        bool // Allow clients to close a reader/writer
	allowFullGET      bool // Allow serving the file via a normal GET request
	allowPUT          bool // Allow writing the file via a PUT requests
	discloseFilenames bool // Allow disclosing filename via Stat()
	closeReaders      bool // Attempt to detect io.Closer and close the io.ReaderAt.
	closeWriters      bool // Attempt to detect io.Closer and close the io.WriterAt.
	mu                sync.RWMutex
	logger            *slog.Logger
}

// NewFileServer creates a new FileServer with a given shared secret
func NewFileServer(urlPrefix, sharedSecret string) (fs *FileServer) {
	fs = &FileServer{
		urlPrefix:         urlPrefix,
		sharedSecret:      sharedSecret,
		readers:           make(map[FileID]*concurrentReadSeeker),
		writers:           make(map[FileID]*concurrentWriteSeeker),
		allowStat:         true,
		allowClose:        true,
		allowFullGET:      true,
		allowPUT:          true,
		discloseFilenames: true,
		closeReaders:      true,
		closeWriters:      true,
		logger:            slog.Default(),
	}

	return fs
}

// SetLogger sets a new structured logger, replacing the default slog logger
func (fs *FileServer) SetLogger(logger *slog.Logger) {
	fs.logger = logger
}

// AllowStat sets whether it is allowed to stat a file by clients and thus divulge more information about it
func (fs *FileServer) AllowStat(allow bool) {
	fs.allowStat = allow
}

// AllowClose sets whether it is allowed to close a file by clients
func (fs *FileServer) AllowClose(allow bool) {
	fs.allowClose = allow
}

// AllowFullGET sets whether to allow serving the file via a normal GET request
func (fs *FileServer) AllowFullGET(allow bool) {
	fs.allowFullGET = allow
}

// AllowPUT sets whether to allow writing to a file using a single raw PUT request
func (fs *FileServer) AllowPUT(allow bool) {
	fs.allowPUT = allow
}

// DiscloseFilenames sets whether the real filenames should be disclosed on Stat()
func (fs *FileServer) DiscloseFilenames(disclose bool) {
	fs.discloseFilenames = disclose
}

// CloseIO allows setting whether the server should attempt to close io.ReaderAts and io.WriterAts once its done with them
func (fs *FileServer) CloseIO(closeReaders, closeWriters bool) {
	fs.closeReaders = closeReaders
	fs.closeWriters = closeWriters
}

// SharedSecret returns the shared secret
func (fs *FileServer) SharedSecret() string {
	return fs.sharedSecret
}

// ServeHTTP is called for each incoming http request and handles the routing and sharedSecret check
func (fs *FileServer) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	if fs.urlPrefix != "" && !strings.HasPrefix(req.URL.Path, fs.urlPrefix) {
		fs.logger.Debug("networkfile.FileServer.ServeHTTP: Invalid URL prefix",
			"url", req.URL.Path, "prefix", fs.urlPrefix)
		resp.WriteHeader(http.StatusBadRequest)
		return
	}

	secret := req.Header.Get(HeaderSharedSecret)
	if secret == "" {
		secret = req.URL.Query().Get(GETSharedSecret)
	}
	if secret != fs.sharedSecret {
		fs.logger.Debug("networkfile.FileServer.ServeHTTP: Invalid secret")
		resp.WriteHeader(http.StatusUnauthorized)
		return
	}

	url := strings.TrimPrefix(req.URL.Path, fs.urlPrefix)
	// The expected URL format is /:fileID here.
	if url[:1] != "/" {
		fs.logger.Debug("networkfile.FileServer.ServeHTTP: Invalid URL prefix", "url", req.URL.Path)
		resp.WriteHeader(http.StatusBadRequest)
		return
	}

	fileID := FileID(url[1:])
	switch req.Method {
	case http.MethodOptions:
		fs.handleFileOptions(resp, fileID)
	case http.MethodGet:
		fs.handleReadFile(resp, req, fileID)
	case http.MethodPatch:
		fs.handleWriteFile(resp, req, fileID)
	case http.MethodPut:
		fs.handleFullWriteFile(resp, req, fileID)
	case http.MethodDelete:
		fs.handleCloseFile(resp, fileID)
	default:
		fs.logger.Debug("networkfile.FileServer.ServeHTTP: Invalid method", "method", req.Method)
		resp.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// ServeFileReader makes the given Reader available under the given FileID
func (fs *FileServer) ServeFileReader(ctx context.Context, fileID FileID, file io.ReadSeeker) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.readers[fileID] != nil {
		return ErrFileIDTaken
	}

	// Make sure we start at offset 0
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	fs.readers[fileID] = &concurrentReadSeeker{
		rdr: file,
	}

	go func() {
		// Wait for the context to expire, then close the reader if it hasnt been already
		<-ctx.Done()

		fs.mu.Lock()
		if fs.closeReader(fileID) {
			fs.logger.Info("networkfile.FileServer.ServeFileReader: Context for file reader expired", "fileID", fileID)
		}
		fs.mu.Unlock()
	}()
	return nil
}

// ServeFileWriter makes the given Writer available under the given FileID
func (fs *FileServer) ServeFileWriter(ctx context.Context, fileID FileID, file io.WriteSeeker) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.writers[fileID] != nil {
		return ErrFileIDTaken
	}

	// Make sure we start at offset 0
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	fs.writers[fileID] = &concurrentWriteSeeker{
		wrtr: file,
	}

	go func() {
		// Wait for the context to expire, then close the writer if it hasnt been already
		<-ctx.Done()

		fs.mu.Lock()
		if fs.closeWriter(fileID) {
			fs.logger.Info("networkfile.FileServer.ServeFileWriter: Context for file writer expired", "fileID", fileID)
		}
		fs.mu.Unlock()
	}()

	return nil
}

// statFile attempts to stat the opened reader/writer to retrieve file information
func (fs *FileServer) statFile(fileID FileID) (FileInfo, error) {
	if !fs.allowStat {
		return FileInfo{}, ErrUnsupportedOperation
	}

	var handle interface{}
	fs.mu.RLock()
	reader := fs.readers[fileID]
	if reader != nil {
		handle = reader.rdr
	}
	if handle == nil {
		writer := fs.writers[fileID]
		if writer != nil {
			handle = writer.wrtr
		}
	}
	fs.mu.RUnlock()

	if handle == nil {
		return FileInfo{}, ErrUnknownFile
	}

	file, ok := handle.(Statter)
	if !ok {
		return FileInfo{}, ErrUnsupportedOperation
	}
	fi, err := file.Stat()
	if err != nil {
		fs.logger.Error("networkfile.FileServer.statFile: Error statting handle", "fileID", fileID, "error", err)
		return FileInfo{}, err
	}

	info := GetFileInfo(fi)
	if !fs.discloseFilenames {
		info.FileName = string(fileID)
	}
	return info, nil
}

// handleFileOptions handles stat requests from the remote reader/writer
func (fs *FileServer) handleFileOptions(resp http.ResponseWriter, fileID FileID) {
	info, err := fs.statFile(fileID)
	if err != nil {
		fs.logger.Debug("networkfile.FileServer.handleFileOptions: Error statting reader", "error", err)
		writeErrorToResponseWriter(resp, err)
		return
	}

	resp.WriteHeader(http.StatusOK)
	resp.Header().Set(HeaderContentLength, fmt.Sprintf("%d", info.FileSize))

	data, err := json.Marshal(&info)
	if err != nil {
		fs.logger.Error("networkfile.FileServer.handleFileOptions: Error marshalling json", "error", err)
		resp.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = resp.Write(data)
	if err != nil {
		fs.logger.Error("networkfile.FileServer.handleFileOptions: Error writing json", "error", err)
	}
}

// requestOffsetAndLength retrieves the offset and length of the read/write requests
func (fs *FileServer) requestOffsetAndLength(resp http.ResponseWriter, req *http.Request) (offset, length int64, ok bool) {
	byteRange := req.Header.Get(HeaderRange)

	matches, err := fmt.Sscanf(byteRange, "%d-%d", &offset, &length)
	if err != nil || matches != 2 {
		fs.logger.Debug("networkfile.FileServer.requestOffsetAndLength: Error parsing range header",
			"byteRange", byteRange, "error", err)
		resp.WriteHeader(http.StatusBadRequest)
		_, _ = resp.Write([]byte("error parsing range header"))
		return 0, 0, false
	}

	if offset < 0 {
		fs.logger.Debug("networkfile.FileServer.requestOffsetAndLength: Invalid offset", "offset", offset)
		resp.WriteHeader(http.StatusBadRequest)
		_, _ = resp.Write([]byte("invalid offset"))
		return 0, 0, false
	}

	if length < MinumumBufferSize {
		fs.logger.Debug("networkfile.FileServer.requestOffsetAndLength: Invalid buffer length", "length", length)
		resp.WriteHeader(http.StatusBadRequest)
		_, _ = resp.Write([]byte("invalid buffer length"))
		return 0, 0, false
	}
	return offset, length, true
}

// handleReadFile handles read http requests from the remote reader
func (fs *FileServer) handleReadFile(resp http.ResponseWriter, req *http.Request, fileID FileID) {
	fs.mu.RLock()
	reader := fs.readers[fileID]
	fs.mu.RUnlock()

	if reader == nil {
		resp.WriteHeader(http.StatusNotFound)
		return
	}

	if fs.allowFullGET && req.Header.Get(HeaderRange) == "" {
		// If the special range header is not set, treat it like a normal GET request
		// Serve the file with the Go http handler to support partial requests
		http.ServeContent(resp, req, string(fileID), time.Now(), reader.New())
		return
	}

	offset, length, ok := fs.requestOffsetAndLength(resp, req)
	if !ok {
		return
	}

	resp.WriteHeader(http.StatusPartialContent)

	rdr := reader.New()
	_, err := rdr.Seek(offset, io.SeekStart)
	if err != nil {
		fs.logger.Error("networkfile.FileServer.handleReadFile: Error seeking to offset",
			"offset", offset, "error", err)
		writeErrorToResponseWriter(resp, err)
		return
	}

	n, err := io.Copy(resp, io.LimitReader(rdr, length))
	if err != nil && !errors.Is(err, io.EOF) {
		fs.logger.Debug("networkfile.FileServer.handleReadFile: Error copying to response",
			"fileID", fileID, "error", err)
		return
	}

	fs.logger.Debug("networkfile.FileServer.handleReadFile: Read bytes", "bytes", n, "offset", offset, "fileID", fileID, "error", err)
}

// handleWriteFile handles write http requests from the remote writer
func (fs *FileServer) handleWriteFile(resp http.ResponseWriter, req *http.Request, fileID FileID) {
	offset, length, ok := fs.requestOffsetAndLength(resp, req)
	if !ok {
		return
	}

	fs.mu.RLock()
	writer := fs.writers[fileID]
	fs.mu.RUnlock()

	if writer == nil {
		resp.WriteHeader(http.StatusNotFound)
		return
	}

	wrtr := writer.New()
	fs.logger.Debug("networkfile.FileServer.handleWriteFile: Seeking", "offset", offset)
	_, err := wrtr.Seek(offset, io.SeekStart)
	if err != nil {
		fs.logger.Error("networkfile.FileServer.handleWriteFile: Error seeking", "offset", offset, "error", err)
		writeErrorToResponseWriter(resp, err)
		return
	}

	n, err := io.Copy(wrtr, req.Body)
	if err != nil {
		fs.logger.Error("networkfile.FileServer.handleWriteFile: Error writing", "error", err)
		writeErrorToResponseWriter(resp, err)
		return
	}

	if n != length {
		fs.logger.Debug("networkfile.FileServer.handleWriteFile: Invalid body length", "copied", n, "length", length)
		resp.WriteHeader(http.StatusBadRequest)
		_, _ = resp.Write([]byte("invalid body length"))
		return
	}

	fs.logger.Debug("networkfile.FileServer.handleWriteFile: Wrote bytes", "bytes", n, "offset", offset, "fileID", fileID)

	resp.Header().Set(HeaderRange, fmt.Sprintf("%d-%d", offset, n))
	resp.WriteHeader(http.StatusNoContent)
}

func (fs *FileServer) handleFullWriteFile(resp http.ResponseWriter, req *http.Request, fileID FileID) {
	if !fs.allowPUT {
		resp.WriteHeader(http.StatusForbidden)
		return
	}

	fs.mu.RLock()
	writer := fs.writers[fileID]
	fs.mu.RUnlock()

	if writer == nil {
		resp.WriteHeader(http.StatusNotFound)
		return
	}

	writer.mu.Lock()
	defer writer.mu.Unlock()

	n, err := io.Copy(writer.wrtr, req.Body)
	if err != nil {
		fs.logger.Error("networkfile.FileServer.handleFullWriteFile: Error writing to writer", "fileID", fileID, "error", err)
		writeErrorToResponseWriter(resp, err)
		return
	}

	fs.logger.Debug("networkfile.FileServer.handleFullWriteFile: Wrote bytes", "bytes", n)
	resp.WriteHeader(http.StatusNoContent)
}

// handleCloseFile handles http requests to close a reader/writer
func (fs *FileServer) handleCloseFile(resp http.ResponseWriter, fileID FileID) {
	if !fs.allowClose {
		resp.WriteHeader(http.StatusForbidden)
		return
	}
	closed := 0

	fs.mu.Lock()
	if fs.closeReader(fileID) {
		closed++
	}
	if fs.closeWriter(fileID) {
		closed++
	}
	fs.mu.Unlock()

	if closed == 0 {
		resp.WriteHeader(http.StatusNotFound)
		return
	}
	resp.WriteHeader(http.StatusNoContent)
}

// closeReader closes and removes a reader, assumes a full lock is held
func (fs *FileServer) closeReader(fileID FileID) bool {
	if fs.readers[fileID] == nil {
		return false
	}

	if fs.closeReaders {
		closer, ok := fs.readers[fileID].rdr.(io.Closer)
		if ok {
			fs.logger.Debug("networkfile.FileServer.closeReader: Closer detected, closing", "fileID", fileID)
			err := closer.Close()
			if err != nil {
				fs.logger.Error("networkfile.FileServer.closeReader: Error closing reader", "fileID", fileID, "error", err)
			}
		}
	}

	delete(fs.readers, fileID)
	return true
}

// closeWriter closes and removes a writer, assumes a full lock is held
func (fs *FileServer) closeWriter(fileID FileID) bool {
	if fs.writers[fileID] == nil {
		return false
	}

	if fs.closeWriters {
		closer, ok := fs.writers[fileID].wrtr.(io.Closer)
		if ok {
			fs.logger.Debug("networkfile.FileServer.closeWriter: Closer detected, closing", "fileID", fileID)
			err := closer.Close()
			if err != nil {
				fs.logger.Error("networkfile.FileServer.closeWriter: Error closing writer", "fileID", fileID, "error", err)
			}
		}
	}

	delete(fs.writers, fileID)
	return true
}
