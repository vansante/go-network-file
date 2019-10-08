package networkfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/julienschmidt/httprouter"
)

var (
	ErrFileIDTaken = errors.New("fileID is already being used")
)

const (
	// MaximumBufferSize is the maximum read size for one HTTP call
	MaximumBufferSize = 10 * 1024 * 1024

	// MaximumBufferSize is the minimum read size for one HTTP call
	MinumumBufferSize = 10

	// HeaderSharedSecret is the name of the header where the shared secret is passed
	HeaderSharedSecret = "X-SharedSecret"

	// HeaderIsEOF is the name of the header which indicates whether the EOF (end of file) has been reached
	HeaderIsEOF = "X-IsEOF"

	// HeaderRange is the header used for sending ranges
	HeaderRange = "X-Range"

	// HeaderContentLength is the header used for sending the file size
	HeaderContentLength = "Content-Length"
)

// ReaderAtCloser combines the io.ReaderAt and io.Closer interfaces
type ReaderAtCloser interface {
	io.ReaderAt
	io.Closer
}

// WriterAtCloser combines the io.WriterAt and io.Closer interfaces
type WriterAtCloser interface {
	io.WriterAt
	io.Closer
}

// FileServer is a HTTP server that serves files as io.Readers or io.Writers
type FileServer struct {
	embedLogger
	sharedSecret      string
	discloseFilenames bool // Allow disclosing filename via Stat()
	server            *http.Server
	router            *httprouter.Router
	readers           map[FileID]ReaderAtCloser
	writers           map[FileID]WriterAtCloser
	mu                sync.RWMutex
}

// NewFileServer creates a new FileServer with a given shared secret
func NewFileServer(sharedSecret string) (fs *FileServer) {
	fs = &FileServer{
		sharedSecret:      sharedSecret,
		discloseFilenames: true,
		router:            httprouter.New(),
		readers:           make(map[FileID]ReaderAtCloser),
		writers:           make(map[FileID]WriterAtCloser),
	}

	fs.router.OPTIONS("/:fileID", fs.checkSecret(fs.fileOptions))
	fs.router.GET("/:fileID", fs.checkSecret(fs.readFile))
	fs.router.PATCH("/:fileID", fs.checkSecret(fs.writeFile))
	fs.router.DELETE("/:fileID", fs.checkSecret(fs.closeFile))

	fs.router.NotFound = http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		fs.Debugf("FileServer: %s %s [404 not found]", req.Method, req.URL.String())
		resp.WriteHeader(http.StatusNotFound)
	})
	fs.router.MethodNotAllowed = http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		fs.Debugf("FileServer: %s %s [405 method not allowed]", req.Method, req.URL.String())
		resp.WriteHeader(http.StatusMethodNotAllowed)
	})

	fs.server = &http.Server{Handler: fs.router}
	return fs
}

// DiscloseFilenames sets whether the real filenames should be disclosed on Stat()
func (fs *FileServer) DiscloseFilenames(secret bool) {
	fs.discloseFilenames = secret
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

// ServeFileReader makes the given Reader available under the given FileID
func (fs *FileServer) ServeFileReader(ctx context.Context, fileID FileID, file ReaderAtCloser) (err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.readers[fileID] != nil {
		return ErrFileIDTaken
	}
	fs.readers[fileID] = file

	// TODO: Handle ctx.Done()

	return nil
}

// ServeFileReader makes the given Writer available under the given FileID
func (fs *FileServer) ServeFileWriter(ctx context.Context, fileID FileID, file WriterAtCloser) (err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.writers[fileID] != nil {
		return ErrFileIDTaken
	}
	fs.writers[fileID] = file

	// TODO: Handle ctx.Done()

	return nil
}

// checkSecret is a HTTP middleware function that checks if the call was done with the right shared secret
// Returns HTTP 401 otherwise.
func (fs *FileServer) checkSecret(subHandler httprouter.Handle) httprouter.Handle {
	return func(resp http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if req.Header.Get(HeaderSharedSecret) != fs.sharedSecret {
			fs.Debugf("FileServer.checkSecret: Invalid secret")
			resp.WriteHeader(http.StatusUnauthorized)
			return
		}

		subHandler(resp, req, params)
	}
}

func (fs *FileServer) statFile(fileID FileID) (info FileInfo, err error) {
	fs.mu.RLock()
	reader := fs.readers[fileID]
	fs.mu.RUnlock()

	if reader == nil {
		return info, ErrUnknownFile
	}

	file, ok := reader.(Statter)
	if !ok {
		return info, ErrUnsupportedOperation
	}
	fi, err := file.Stat()
	if err != nil {
		fs.Errorf("FileServer.statFile: Error statting reader %s: %v", fileID, err)
		return info, err
	}

	info = GetFileInfo(fi)
	if !fs.discloseFilenames {
		info.FileName = string(fileID)
	}
	return info, nil
}

func (fs *FileServer) fileOptions(resp http.ResponseWriter, req *http.Request, params httprouter.Params) {
	fileID := FileID(params.ByName("fileID"))
	info, err := fs.statFile(fileID)
	if err != nil {
		fs.Debugf("FileServer.statFile: Error statting reader: %v", err)
		writeErrorToResponseWriter(resp, err)
		return
	}

	resp.WriteHeader(http.StatusOK)
	resp.Header().Set(HeaderContentLength, fmt.Sprintf("%d", info.FileSize))

	data, err := json.Marshal(&info)
	if err != nil {
		fs.Errorf("FileServer.statFile: Error marshalling json: %v", err)
		resp.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = resp.Write(data)
	if err != nil {
		fs.Errorf("FileServer.statFile: Error writing json: %v", err)
	}
}

func (fs *FileServer) readFile(resp http.ResponseWriter, req *http.Request, params httprouter.Params) {
	fileID := FileID(params.ByName("fileID"))
	byteRange := req.Header.Get(HeaderRange)

	var offset, length int64
	n, err := fmt.Sscanf(byteRange, "%d-%d", &offset, &length)
	if err != nil || n != 2 {
		fs.Debugf("FileServer.readFile: Error reading range header (%s)", byteRange)
		resp.WriteHeader(http.StatusBadRequest)
		return
	}

	if offset < 0 {
		fs.Debugf("FileServer.readFile: Invalid offset (%d)", offset)
		resp.WriteHeader(http.StatusBadRequest)
		return
	}

	if length < MinumumBufferSize || length > MaximumBufferSize {
		fs.Debugf("FileServer.readFile: Invalid buffer length (%d)", length)
		resp.WriteHeader(http.StatusBadRequest)
		return
	}

	fs.mu.RLock()
	reader := fs.readers[fileID]
	fs.mu.RUnlock()

	if reader == nil {
		resp.WriteHeader(http.StatusNotFound)
		return
	}

	buf := make([]byte, length)
	n, err = reader.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		writeErrorToResponseWriter(resp, err)
		return
	}

	eof := err == io.EOF
	resp.Header().Set(HeaderIsEOF, fmt.Sprintf("%v", eof))
	resp.Header().Set(HeaderRange, fmt.Sprintf("%d-%d", offset, offset+int64(n)))

	fs.Debugf("FileServer.readFile: Read %d bytes from offset %d from file %s [EOF: %v]", n, offset, fileID, eof)

	resp.WriteHeader(http.StatusOK)
	_, err = resp.Write(buf[:n])
	if err != nil {
		fs.Errorf("FileServer.readFile: Error writing buffer to response: %v", err)
	}
}

func (fs *FileServer) writeFile(resp http.ResponseWriter, req *http.Request, params httprouter.Params) {
	//fileID := FileID(params.ByName("fileID"))

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// TODO: FIXME
}

func (fs *FileServer) closeFile(resp http.ResponseWriter, req *http.Request, params httprouter.Params) {
	fileID := FileID(params.ByName("fileID"))
	closed := 0

	fs.mu.Lock()
	if fs.readers[fileID] != nil {
		err := fs.readers[fileID].Close()
		if err != nil {
			fs.Errorf("FileServer.closeFile: Error closing reader %s: %v", fileID, err)
		}

		delete(fs.readers, fileID)
		closed++
	}

	if fs.writers[fileID] != nil {
		err := fs.writers[fileID].Close()
		if err != nil {
			fs.Errorf("FileServer.closeFile: Error closing writer %s: %v", fileID, err)
		}

		delete(fs.writers, fileID)
		closed++
	}
	fs.mu.Unlock()

	if closed == 0 {
		resp.WriteHeader(http.StatusNotFound)
		return
	}
	resp.WriteHeader(http.StatusNoContent)
}
