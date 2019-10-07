package networkfile

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	rand2 "math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	secret = "blurp"
)

func randomFile(size int) (*os.File, error) {
	file, err := ioutil.TempFile(os.TempDir(), "reader-")
	if err != nil {
		return nil, err
	}

	buf := make([]byte, size)

	_, err = rand.Read(buf)
	if err != nil {
		return nil, err
	}

	_, err = file.Write(buf)
	return file, err
}

func newSocketPath() string {
	rand2.Seed(time.Now().UnixNano())
	rnd := rand2.Uint64()
	return filepath.Join(os.TempDir(), fmt.Sprintf("testsock-%d", rnd))
}

func newHTTPUnixClient(socket string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socket)
			},
		},
	}
}

func TestReaderCopyFile(t *testing.T) {
	srv := NewFileServer(secret)
	srv.SetLogger(&testLogger{t})

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.Nil(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	srv.SetLogger(&testLogger{t})

	fileID, err := RandomFileID()
	assert.Nil(t, err)
	src, err := randomFile(137)
	assert.Nil(t, err)

	err = srv.ServeFileReader(context.Background(), fileID, src)

	rdr := NewCustomClientReader(newHTTPUnixClient(socket), "http://server", secret, fileID)
	rdr.SetLogger(&testLogger{t})
	dst, err := ioutil.TempFile(os.TempDir(), "writer-copy-test-")
	assert.Nil(t, err)

	n, err := io.CopyBuffer(dst, rdr, make([]byte, 13))
	assert.Nil(t, err)
	assert.EqualValues(t, 137, n)

	_, err = src.Seek(0, io.SeekStart)
	assert.Nil(t, err)
	srcBuf, err := ioutil.ReadAll(src)
	assert.Nil(t, err)

	_, err = dst.Seek(0, io.SeekStart)
	assert.Nil(t, err)
	dstBuf, err := ioutil.ReadAll(dst)
	assert.Nil(t, err)

	assert.EqualValues(t, srcBuf, dstBuf)

	assert.Nil(t, srv.Shutdown(context.Background()))
}

func TestReaderCopySingleCall(t *testing.T) {
	srv := NewFileServer(secret)
	srv.SetLogger(&testLogger{t})

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.Nil(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	srv.SetLogger(&testLogger{t})

	fileID, err := RandomFileID()
	assert.Nil(t, err)
	src, err := randomFile(13)
	assert.Nil(t, err)

	err = srv.ServeFileReader(context.Background(), fileID, src)

	rdr := NewCustomClientReader(newHTTPUnixClient(socket), "http://server", secret, fileID)
	rdr.SetLogger(&testLogger{t})
	dst, err := ioutil.TempFile(os.TempDir(), "writer-copy-test-")
	assert.Nil(t, err)

	n, err := io.CopyBuffer(dst, rdr, make([]byte, 13))
	assert.Nil(t, err)
	assert.EqualValues(t, 13, n)

	_, err = src.Seek(0, io.SeekStart)
	assert.Nil(t, err)
	srcBuf, err := ioutil.ReadAll(src)
	assert.Nil(t, err)

	_, err = dst.Seek(0, io.SeekStart)
	assert.Nil(t, err)
	dstBuf, err := ioutil.ReadAll(dst)
	assert.Nil(t, err)

	assert.EqualValues(t, srcBuf, dstBuf)

	assert.Nil(t, srv.Shutdown(context.Background()))
}

func TestReaderBadSecret(t *testing.T) {
	srv := NewFileServer(secret)
	srv.SetLogger(&testLogger{t})

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.Nil(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()
	time.Sleep(100 * time.Millisecond)

	rdr := NewCustomClientReader(newHTTPUnixClient(socket), "http://server", "wrong", "test")
	rdr.SetLogger(&testLogger{t})
	n, err := rdr.Read(make([]byte, 11))
	assert.EqualValues(t, 0, n)
	assert.Equal(t, ErrUnauthorized, err)

	assert.Nil(t, srv.Shutdown(context.Background()))
}

func TestReaderUnknownFile(t *testing.T) {
	srv := NewFileServer(secret)
	srv.SetLogger(&testLogger{t})

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.Nil(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()
	time.Sleep(100 * time.Millisecond)

	rdr := NewCustomClientReader(newHTTPUnixClient(socket), "http://server", secret, "test")
	rdr.SetLogger(&testLogger{t})
	n, err := rdr.Read(make([]byte, 11))
	assert.EqualValues(t, 0, n)
	assert.Equal(t, ErrUnknownFile, err)

	assert.Nil(t, srv.Shutdown(context.Background()))
}

type testLogger struct {
	*testing.T
}

func (tl *testLogger) Debugf(format string, args ...interface{}) {
	log.Printf("[DEBUG] "+format, args...)
	//tl.Logf("[DEBUG] "+format, args...)
}

func (tl *testLogger) Infof(format string, args ...interface{}) {
	log.Printf("[INFO] "+format, args...)
	//tl.Logf("[INFO] "+format, args...)
}

func (tl *testLogger) Errorf(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
	//tl.Logf("[ERROR] "+format, args...)
}
