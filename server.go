package networkfile

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/julienschmidt/httprouter"
)

var (
	ErrFileIDTaken = errors.New("fileID is already being used")
)

const (
	// MaximumBufferSize is the maximum read size for one HTTP call
	MaximumBufferSize = 10 * 1024 * 1024

	// SharedSecretHeader is the name of the header where the shared secret is passed
	SharedSecretHeader = "X-SharedSecret"
)

// FileID is a unique descriptor for a file, should be usable in an URL.
type FileID string

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

type FileServer struct {
	embedLogger
	sharedSecret string
	server       *http.Server
	router       *httprouter.Router
	readers      map[FileID]ReaderAtCloser
	writers      map[FileID]WriterAtCloser
	mu           sync.RWMutex
	logger       Logger
}

func NewFileServer(sharedSecret string) (fs *FileServer) {
	fs = &FileServer{
		sharedSecret: sharedSecret,
		router:       httprouter.New(),
		readers:      make(map[FileID]ReaderAtCloser),
		writers:      make(map[FileID]WriterAtCloser),
	}

	fs.router.GET("/:fileID/read/:offset/:length", fs.checkSecret(fs.readFile))
	fs.router.PUT("/:fileID/write/:offset", fs.checkSecret(fs.writeFile))
	fs.router.POST("/:fileID/close", fs.checkSecret(fs.closeFile))

	fs.server = &http.Server{Handler: fs.router}
	return fs
}

func (fs *FileServer) Serve(socket net.Listener) (err error) {
	return fs.server.Serve(socket)
}

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
		if req.Header.Get(SharedSecretHeader) != fs.sharedSecret {
			resp.WriteHeader(http.StatusUnauthorized)
			return
		}

		subHandler(resp, req, params)
	}
}

func (fs *FileServer) readFile(resp http.ResponseWriter, req *http.Request, params httprouter.Params) {
	fileID := FileID(params.ByName("fileID"))
	offset, err := strconv.ParseInt(params.ByName("offset"), 10, 64)
	if err != nil {
		fs.Infof("FileServer.readFile: Bad offset parameter (%s)", params.ByName("offset"))
		resp.WriteHeader(http.StatusBadRequest)
		return
	}

	length, err := strconv.ParseInt(params.ByName("length"), 10, 64)
	if err != nil {
		fs.Infof("FileServer.readFile: Bad length parameter (%s)", params.ByName("length"))
		resp.WriteHeader(http.StatusBadRequest)
		return
	}
	if length > MaximumBufferSize {
		fs.Infof("FileServer.readFile: Length parameter too great (%d)", length)
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
	n, err := reader.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		code, ok := errToHttpCode[err]
		if !ok {
			resp.WriteHeader(HttpCodeUnknownError)
			_, _ = resp.Write([]byte(err.Error()))
			return
		}
		resp.WriteHeader(code)
		return
	}

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
