package networkfile

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	secret = "blurp"
)

func randomFile(size int) (*os.File, error) {
	file, err := os.CreateTemp(os.TempDir(), "rndfile-")
	if err != nil {
		return nil, err
	}

	buf := make([]byte, size)

	_, err = rand.Read(buf)
	if err != nil {
		return nil, err
	}

	_, err = file.Write(buf)
	if err != nil {
		return nil, err
	}
	_, err = file.Seek(0, io.SeekStart)
	return file, err
}

func newSocketPath() string {
	rnd, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt))
	if err != nil {
		panic(err)
	}
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

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	fileID, err := RandomFileID()
	assert.NoError(t, err)
	src, err := randomFile(137)
	assert.NoError(t, err)
	defer func() {
		_ = src.Close()
		_ = os.Remove(src.Name())
	}()

	err = srv.ServeFileReader(context.Background(), fileID, src)

	rdr := NewCustomClientReader(context.Background(), newHTTPUnixClient(socket), "http://server", secret, fileID)

	dst, err := os.CreateTemp(os.TempDir(), "reader-copy-test-")
	assert.Nil(t, err)
	defer func() {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
	}()

	n, err := io.CopyBuffer(dst, rdr, make([]byte, 13))
	assert.NoError(t, err)
	assert.EqualValues(t, 137, n)

	_, err = src.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	srcBuf, err := io.ReadAll(src)
	assert.NoError(t, err)

	_, err = dst.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	dstBuf, err := io.ReadAll(dst)
	assert.NoError(t, err)

	assert.EqualValues(t, srcBuf, dstBuf)

	assert.NoError(t, rdr.Close())
	assert.NoError(t, srv.Shutdown(context.Background()))
}

func TestReaderCopySingleCall(t *testing.T) {
	srv := NewFileServer(secret)

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	fileID, err := RandomFileID()
	assert.NoError(t, err)
	src, err := randomFile(13)
	assert.NoError(t, err)
	defer func() {
		_ = src.Close()
		_ = os.Remove(src.Name())
	}()

	err = srv.ServeFileReader(context.Background(), fileID, src)

	rdr := NewCustomClientReader(context.Background(), newHTTPUnixClient(socket), "http://server", secret, fileID)
	dst, err := os.CreateTemp(os.TempDir(), "reader-single-copy-test-")
	assert.NoError(t, err)
	defer func() {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
	}()

	n, err := io.CopyBuffer(dst, rdr, make([]byte, 13))
	assert.NoError(t, err)
	assert.EqualValues(t, 13, n)

	_, err = src.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	srcBuf, err := io.ReadAll(src)
	assert.NoError(t, err)

	_, err = dst.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	dstBuf, err := io.ReadAll(dst)
	assert.NoError(t, err)

	assert.EqualValues(t, srcBuf, dstBuf)

	assert.NoError(t, rdr.Close())
	assert.NoError(t, srv.Shutdown(context.Background()))
}

func TestReaderSeek(t *testing.T) {
	srv := NewFileServer(secret)

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	fileID, err := RandomFileID()
	assert.NoError(t, err)
	src, err := randomFile(183)
	assert.NoError(t, err)

	_, err = src.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	srcBuf, err := io.ReadAll(src)
	assert.NoError(t, err)

	err = srv.ServeFileReader(context.Background(), fileID, src)

	rdr := NewCustomClientReader(context.Background(), newHTTPUnixClient(socket), "http://server", secret, fileID)

	off, err := rdr.Seek(2, io.SeekStart)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, off)

	buf := make([]byte, 11)
	n, err := rdr.Read(buf)
	assert.EqualValues(t, 11, n)
	assert.NoError(t, err)
	assert.EqualValues(t, srcBuf[2:2+11], buf)

	off, err = rdr.Seek(-2, io.SeekCurrent)
	assert.NoError(t, err)
	assert.EqualValues(t, 2+11-2, off)

	n, err = rdr.Read(buf)
	assert.EqualValues(t, 11, n)
	assert.NoError(t, err)
	assert.EqualValues(t, srcBuf[2+11-2:2+11-2+11], buf)

	off, err = rdr.Seek(-37, io.SeekEnd)
	assert.NoError(t, err)
	assert.EqualValues(t, 183-37, off)

	n, err = rdr.Read(buf)
	assert.EqualValues(t, 11, n)
	assert.NoError(t, err)
	assert.EqualValues(t, srcBuf[183-37:183-37+11], buf)

	assert.NoError(t, rdr.Close())
	assert.NoError(t, srv.Shutdown(context.Background()))
}

func TestFullGetRead(t *testing.T) {
	srv := NewFileServer(secret)

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	fileID, err := RandomFileID()
	assert.NoError(t, err)
	src, err := randomFile(11325)
	assert.NoError(t, err)

	err = srv.ServeFileReader(context.Background(), fileID, src)
	assert.NoError(t, err)

	client := newHTTPUnixClient(socket)
	rdr := NewCustomClientReader(context.Background(), client, "http://server", secret, fileID)
	resp, err := client.Get(rdr.FullReadURL())
	assert.NoError(t, err)

	_, err = src.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	srcBuf, err := io.ReadAll(src)
	assert.NoError(t, err)

	file, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	assert.EqualValues(t, file, srcBuf)
}

func TestMultipleFullGetRead(t *testing.T) {
	srv := NewFileServer(secret)

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	fileID, err := RandomFileID()
	assert.NoError(t, err)
	src, err := randomFile(113025)
	assert.NoError(t, err)

	err = srv.ServeFileReader(context.Background(), fileID, src)
	assert.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(100)
	for i := 0; i < 100; i++ {
		go func() {
			client := newHTTPUnixClient(socket)
			rdr := NewCustomClientReader(context.Background(), client, "http://server", secret, fileID)
			resp, err := client.Get(rdr.FullReadURL())
			assert.NoError(t, err)
			if err != nil {
				return
			}

			_, err = io.ReadAll(resp.Body)
			assert.NoError(t, err)
			_ = resp.Body.Close()
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestReaderContextExpires(t *testing.T) {
	srv := NewFileServer(secret)

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	fileID, err := RandomFileID()
	assert.NoError(t, err)
	src, err := randomFile(11325)
	assert.NoError(t, err)

	// ensure this will expire
	ctx, cancel := context.WithTimeout(context.Background(), time.Microsecond)
	defer cancel()

	err = srv.ServeFileReader(ctx, fileID, src)
	assert.NoError(t, err)

	client := newHTTPUnixClient(socket)
	resp, err := client.Get(fmt.Sprintf("http://server/%s?%s=%s", fileID, GETSharedSecret, secret))
	assert.NoError(t, err)
	_ = resp.Body.Close()

	assert.EqualValues(t, http.StatusNotFound, resp.StatusCode)
}

func TestReaderBadSecret(t *testing.T) {
	srv := NewFileServer(secret)

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()
	time.Sleep(100 * time.Millisecond)

	rdr := NewCustomClientReader(context.Background(), newHTTPUnixClient(socket), "http://server", "wrong", "test")
	n, err := rdr.Read(make([]byte, 11))
	assert.EqualValues(t, 0, n)
	assert.Equal(t, ErrUnauthorized, err)

	assert.Error(t, rdr.Close())
	assert.NoError(t, srv.Shutdown(context.Background()))
}

func TestReaderUnknownFile(t *testing.T) {
	srv := NewFileServer(secret)

	socket := newSocketPath()
	sock, err := net.Listen("unix", socket)
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()
	time.Sleep(100 * time.Millisecond)

	rdr := NewCustomClientReader(context.Background(), newHTTPUnixClient(socket), "http://server", secret, "test")
	n, err := rdr.Read(make([]byte, 11))
	assert.EqualValues(t, 0, n)
	assert.Equal(t, ErrUnknownFile, err)

	assert.Error(t, rdr.Close())
	assert.NoError(t, srv.Shutdown(context.Background()))
}

func TestReaderLargeFile(t *testing.T) {
	srv := NewFileServer(secret)

	sock, err := net.Listen("tcp", ":1337")
	assert.NoError(t, err)
	go func() {
		err = srv.Serve(sock)
		assert.EqualValues(t, http.ErrServerClosed, err)
	}()

	fileID, err := RandomFileID()
	assert.NoError(t, err)
	src, err := randomFile(1337 * 1337 * 13)
	assert.NoError(t, err)
	defer func() {
		_ = src.Close()
		_ = os.Remove(src.Name())
	}()

	err = srv.ServeFileReader(context.Background(), fileID, src)

	rdr := NewReader(context.Background(), "http://localhost:1337", secret, fileID)
	dst, err := os.CreateTemp(os.TempDir(), "reader-single-copy-test-")
	assert.NoError(t, err)
	defer func() {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
	}()

	n, err := io.CopyBuffer(dst, rdr, make([]byte, 32*1024))
	assert.NoError(t, err)
	assert.EqualValues(t, 1337*1337*13, n)
}
