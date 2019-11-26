package networkfile

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	ErrFileIDTaken = errors.New("fileID is already being used")
)

const (
	// MaximumBufferSize is the minimum buffer size for one HTTP call
	MinumumBufferSize = 1

	// HeaderSharedSecret is the name of the header where the shared secret is passed
	HeaderSharedSecret = "X-SharedSecret"

	// GETSharedSecret is the name of the GET parameter that is also an acceptable way of transferring the shared secret
	GETSharedSecret = "shared-secret"

	// HeaderIsEOF is the name of the header which indicates whether the EOF (end of file) has been reached
	HeaderIsEOF = "X-IsEOF"

	// HeaderRange is the header used for sending ranges
	HeaderRange = "X-Range"

	// HeaderContentLength is the header used for sending the file size
	HeaderContentLength = "Content-Length"

	// DefaultWriteBufferSize is the default buffer size used while writing
	DefaultWriteBufferSize = 256 * 1024
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

// FileServer is a HTTP server that serves files as io.Readers or io.Writers
type FileServer struct {
	embedLogger
	sharedSecret      string
	server            *http.Server
	readers           map[FileID]io.ReaderAt
	writers           map[FileID]io.WriterAt
	allowStat         bool // Allow disclosing the information of Stat()
	allowClose        bool // Allow clients to close a reader/writer
	allowNormalGET    bool // Allow serving the file via a normal GET request
	discloseFilenames bool // Allow disclosing filename via Stat()
	writeBufferSize   int
	mu                sync.RWMutex
}

// NewFileServer creates a new FileServer with a given shared secret
func NewFileServer(sharedSecret string) (fs *FileServer) {
	fs = &FileServer{
		sharedSecret:      sharedSecret,
		readers:           make(map[FileID]io.ReaderAt),
		writers:           make(map[FileID]io.WriterAt),
		allowStat:         true,
		allowClose:        true,
		allowNormalGET:    true,
		discloseFilenames: true,
		writeBufferSize:   DefaultWriteBufferSize,
	}

	fs.server = &http.Server{Handler: fs}
	return fs
}

// AllowStat sets whether it is allowed to stat a file by clients and thus divulge more information about it
func (fs *FileServer) AllowStat(allow bool) {
	fs.allowStat = allow
}

// AllowClose sets whether it is allowed to close a file by clients
func (fs *FileServer) AllowClose(allow bool) {
	fs.allowClose = allow
}

// AllowNormalGET sets whether to allow serving the file via a normal GET request
func (fs *FileServer) AllowNormalGET(allow bool) {
	fs.allowNormalGET = allow
}

// DiscloseFilenames sets whether the real filenames should be disclosed on Stat()
func (fs *FileServer) DiscloseFilenames(secret bool) {
	fs.discloseFilenames = secret
}

// SetWriteBufferSize sets the write buffer size
func (fs *FileServer) SetWriteBufferSize(size int) {
	fs.writeBufferSize = size
}

// Serve starts serving the FileServer over HTTP over the given socket
func (fs *FileServer) Serve(socket net.Listener) (err error) {
	return fs.server.Serve(socket)
}

// Shutdown shuts down the HTTP server gracefully with a context.
// The socket needs to be closed manually
func (fs *FileServer) Shutdown(ctx context.Context) (err error) {
	return fs.server.Shutdown(ctx)
}

// ServeHTTP is called for each incoming http request and handles the routing and sharedSecret check
func (fs *FileServer) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	secret := req.Header.Get(HeaderSharedSecret)
	if secret == "" {
		secret = req.URL.Query().Get(GETSharedSecret)
	}
	if secret != fs.sharedSecret {
		fs.Debugf("FileServer.ServeHTTP: Invalid secret")
		resp.WriteHeader(http.StatusUnauthorized)
		return
	}

	// The expected URL format is /:fileID here.
	url := req.URL.Path
	if url[:1] != "/" {
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
	case http.MethodDelete:
		fs.handleCloseFile(resp, fileID)
	default:
		fs.Debugf("FileServer.ServeHTTP: Invalid method %s", req.Method)
		resp.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// ServeFileReader makes the given Reader available under the given FileID
func (fs *FileServer) ServeFileReader(ctx context.Context, fileID FileID, file io.ReaderAt) (err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.readers[fileID] != nil {
		return ErrFileIDTaken
	}
	fs.readers[fileID] = file

	go func() {
		// Wait for the context to expire, then close the reader if it hasnt been already
		<-ctx.Done()

		fs.mu.Lock()
		if fs.closeReader(fileID) {
			fs.Infof("FileServer.ServeFileReader: Context for file reader expired: %s", fileID)
		}
		fs.mu.Unlock()
	}()
	return nil
}

// ServeFileReader makes the given Writer available under the given FileID
func (fs *FileServer) ServeFileWriter(ctx context.Context, fileID FileID, file io.WriterAt) (err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.writers[fileID] != nil {
		return ErrFileIDTaken
	}
	fs.writers[fileID] = file

	go func() {
		// Wait for the context to expire, then close the writer if it hasnt been already
		<-ctx.Done()

		fs.mu.Lock()
		if fs.closeWriter(fileID) {
			fs.Infof("FileServer.ServeFileWriter: Context for file writer expired: %s", fileID)
		}
		fs.mu.Unlock()
	}()

	return nil
}

// statFile attempts to stat the opened reader/writer to retrieve file information
func (fs *FileServer) statFile(fileID FileID) (info FileInfo, err error) {
	if !fs.allowStat {
		return info, ErrUnsupportedOperation
	}

	var handle interface{}
	fs.mu.RLock()
	handle = fs.readers[fileID]
	if handle == nil {
		handle = fs.writers[fileID]
	}
	fs.mu.RUnlock()

	if handle == nil {
		return info, ErrUnknownFile
	}

	file, ok := handle.(Statter)
	if !ok {
		return info, ErrUnsupportedOperation
	}
	fi, err := file.Stat()
	if err != nil {
		fs.Errorf("FileServer.statFile: Error statting handle %s: %v", fileID, err)
		return info, err
	}

	info = GetFileInfo(fi)
	if !fs.discloseFilenames {
		info.FileName = string(fileID)
	}
	return info, nil
}

// handleFileOptions handles stat requests from the remote reader/writer
func (fs *FileServer) handleFileOptions(resp http.ResponseWriter, fileID FileID) {
	info, err := fs.statFile(fileID)
	if err != nil {
		fs.Debugf("FileServer.handleFileOptions: Error statting reader: %v", err)
		writeErrorToResponseWriter(resp, err)
		return
	}

	resp.WriteHeader(http.StatusOK)
	resp.Header().Set(HeaderContentLength, fmt.Sprintf("%d", info.FileSize))

	data, err := json.Marshal(&info)
	if err != nil {
		fs.Errorf("FileServer.handleFileOptions: Error marshalling json: %v", err)
		resp.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = resp.Write(data)
	if err != nil {
		fs.Errorf("FileServer.handleFileOptions: Error writing json: %v", err)
	}
}

// requestOffsetAndLength retrieves the offset and length of the read/write requests
func (fs *FileServer) requestOffsetAndLength(resp http.ResponseWriter, req *http.Request) (offset, length int64, ok bool) {
	byteRange := req.Header.Get(HeaderRange)

	matches, err := fmt.Sscanf(byteRange, "%d-%d", &offset, &length)
	if err != nil || matches != 2 {
		fs.Debugf("FileServer.requestOffsetAndLength: Error parsing range header (%s): %v", byteRange, err)
		resp.WriteHeader(http.StatusBadRequest)
		_, _ = resp.Write([]byte("error parsing range header"))
		return 0, 0, false
	}

	if offset < 0 {
		fs.Debugf("FileServer.requestOffsetAndLength: Invalid offset (%d)", offset)
		resp.WriteHeader(http.StatusBadRequest)
		_, _ = resp.Write([]byte("invalid offset"))
		return 0, 0, false
	}

	if length < MinumumBufferSize {
		fs.Debugf("FileServer.requestOffsetAndLength: Invalid buffer length (%d)", length)
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

	if fs.allowNormalGET && req.Header.Get(HeaderRange) == "" {
		// If the special range header is not set, treat it like a normal GET request
		readSeeker, ok := reader.(io.ReadSeeker)
		if ok {
			fs.Debugf("FileServer.handleReadFile: Serving normal file with seek capability")
			http.ServeContent(resp, req, string(fileID), time.Now(), readSeeker)
			return
		}
		fs.Debugf("FileServer.handleReadFile: Serving normal file")
		_, err := io.Copy(resp, &ReaderAtReader{
			ReaderAt: reader,
			offset:   0,
		})
		if err != nil {
			fs.Errorf("FileServer.handleReadFile: Error while serving normal file: %v", err)
		}
		return
	}

	offset, length, ok := fs.requestOffsetAndLength(resp, req)
	if !ok {
		return
	}

	buf := make([]byte, length)
	n, err := reader.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		writeErrorToResponseWriter(resp, err)
		return
	}

	eof := err == io.EOF
	resp.Header().Set(HeaderIsEOF, fmt.Sprintf("%v", eof))
	resp.Header().Set(HeaderRange, fmt.Sprintf("%d-%d", offset, n))

	fs.Debugf("FileServer.handleReadFile: Read %d bytes from offset %d from file %s [EOF: %v]", n, offset, fileID, eof)

	resp.WriteHeader(http.StatusPartialContent)
	_, err = resp.Write(buf[:n])
	if err != nil {
		fs.Errorf("FileServer.handleReadFile: Error writing buffer to response: %v", err)
	}
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

	var totalRead, written int64
	buf := make([]byte, fs.writeBufferSize)
	for totalRead < length {
		var read int64
		for read < int64(len(buf)) {
			n, readErr := req.Body.Read(buf[read:])
			read += int64(n)
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					fs.Errorf("FileServer.handleWriteFile: Error reading request body: %v", readErr)
					writeErrorToResponseWriter(resp, readErr)
					return
				}
				break
			}
		}

		n, writeErr := writer.WriteAt(buf[:read], offset+written)
		if writeErr != nil && !errors.Is(writeErr, io.EOF) {
			fs.Errorf("FileServer.handleWriteFile: Error writing to writer: %v", writeErr)
			writeErrorToResponseWriter(resp, writeErr)
			return
		}

		fs.Debugf("FileServer.handleWriteFile: Wrote %d bytes at offset %d", read, offset+written)

		written += int64(n)
		totalRead += read

		if errors.Is(writeErr, io.EOF) {
			break
		}
	}

	if totalRead != length {
		fs.Debugf("FileServer.handleWriteFile: Invalid body length (%d != %d)", totalRead, length)
		resp.WriteHeader(http.StatusBadRequest)
		_, _ = resp.Write([]byte("invalid body length"))
		return
	}

	if totalRead != written {
		fs.Debugf("FileServer.handleWriteFile: Bytes written != bytes read  (%d != %d)", totalRead, written)
		resp.WriteHeader(http.StatusInternalServerError)
		_, _ = resp.Write([]byte("invalid bytes written"))
		return
	}

	fs.Debugf("FileServer.handleWriteFile: Wrote %d bytes from offset %d in file %s", written, offset, fileID)

	resp.Header().Set(HeaderRange, fmt.Sprintf("%d-%d", offset, written))
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

	closer, ok := fs.readers[fileID].(io.Closer)
	if ok {
		fs.Debugf("FileServer.closeReader: Closer detected, closing: %s", fileID)
		err := closer.Close()
		if err != nil {
			fs.Errorf("FileServer.closeReader: Error closing reader %s: %v", fileID, err)
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

	closer, ok := fs.writers[fileID].(io.Closer)
	if ok {
		fs.Debugf("FileServer.closeWriter: Closer detected, closing: %s", fileID)
		err := closer.Close()
		if err != nil {
			fs.Errorf("FileServer.closeWriter: Error closing writer %s: %v", fileID, err)
		}
	}

	delete(fs.writers, fileID)
	return true
}
