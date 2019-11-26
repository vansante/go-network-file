package networkfile

import (
	"context"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriterCopyFile(t *testing.T) {
	srv := NewFileServer(secret)
	srv.SetLogger(&testLogger{t})

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	fileID, err := RandomFileID()
	assert.NoError(t, err)
	dst, err := ioutil.TempFile(os.TempDir(), "writer-copy-test-")
	assert.NoError(t, err)
	defer func() {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
	}()

	err = srv.ServeFileWriter(context.Background(), fileID, dst)
	assert.NoError(t, err)

	wrtr := NewCustomClientWriter(newHTTPUnixClient(socket), "http://server", secret, fileID)
	wrtr.SetLogger(&testLogger{t})

	src, err := randomFile(117)
	assert.NoError(t, err)
	defer func() {
		_ = src.Close()
		_ = os.Remove(src.Name())
	}()

	n, err := io.CopyBuffer(wrtr, src, make([]byte, 17))
	assert.NoError(t, err)
	assert.EqualValues(t, 117, n)

	_, err = dst.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	dstBuf, err := ioutil.ReadAll(dst)
	assert.NoError(t, err)

	_, err = src.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	srcBuf, err := ioutil.ReadAll(src)
	assert.NoError(t, err)

	assert.EqualValues(t, srcBuf, dstBuf)

	assert.NoError(t, wrtr.Close())
	assert.NoError(t, srv.Shutdown(context.Background()))
}

func TestWriterCopyFileLargeBuffer(t *testing.T) {
	srv := NewFileServer(secret)
	srv.SetLogger(&testLogger{t})

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	fileID, err := RandomFileID()
	assert.NoError(t, err)
	dst, err := ioutil.TempFile(os.TempDir(), "writer-copy-test-")
	assert.NoError(t, err)
	defer func() {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
	}()

	err = srv.ServeFileWriter(context.Background(), fileID, dst)
	assert.NoError(t, err)

	wrtr := NewCustomClientWriter(newHTTPUnixClient(socket), "http://server", secret, fileID)
	wrtr.SetLogger(&testLogger{t})

	src, err := randomFile(17_177_717)
	assert.NoError(t, err)
	defer func() {
		_ = src.Close()
		_ = os.Remove(src.Name())
	}()

	n, err := io.CopyBuffer(wrtr, src, make([]byte, 1_577_777))
	assert.NoError(t, err)
	assert.EqualValues(t, 1_577_777, n)

	_, err = dst.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	dstBuf, err := ioutil.ReadAll(dst)
	assert.NoError(t, err)

	_, err = src.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	srcBuf, err := ioutil.ReadAll(src)
	assert.NoError(t, err)

	assert.EqualValues(t, srcBuf, dstBuf)

	assert.NoError(t, wrtr.Close())
	assert.NoError(t, srv.Shutdown(context.Background()))
}
