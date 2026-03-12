package networkfile

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriterCopyFile(t *testing.T) {
	srv := NewFileServer(prefix, secret)
	testServer := httptest.NewServer(srv)

	fileID, err := RandomFileID()
	require.NoError(t, err)
	dst, err := os.CreateTemp(os.TempDir(), "writer-copy-test-")
	require.NoError(t, err)
	defer func() {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
	}()

	err = srv.ServeFileWriter(context.Background(), fileID, dst)
	require.NoError(t, err)

	wrtr := NewWriter(context.Background(), testServer.URL+prefix, secret, fileID)

	src, err := randomFile(117)
	require.NoError(t, err)
	defer func() {
		_ = src.Close()
		_ = os.Remove(src.Name())
	}()

	n, err := io.CopyBuffer(wrtr, src, make([]byte, 17))
	require.NoError(t, err)
	assert.EqualValues(t, 117, n)

	_, err = dst.Seek(0, io.SeekStart)
	require.NoError(t, err)
	dstBuf, err := io.ReadAll(dst)
	require.NoError(t, err)

	_, err = src.Seek(0, io.SeekStart)
	require.NoError(t, err)
	srcBuf, err := io.ReadAll(src)
	require.NoError(t, err)

	assert.Equal(t, srcBuf, dstBuf)

	assert.NoError(t, wrtr.Close())
}

func TestWriterCopyFileLargeBuffer(t *testing.T) {
	srv := NewFileServer(prefix, secret)
	testServer := httptest.NewServer(srv)

	fileID, err := RandomFileID()
	require.NoError(t, err)
	dst, err := os.CreateTemp(os.TempDir(), "writer-large-test-")
	require.NoError(t, err)
	defer func() {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
	}()

	err = srv.ServeFileWriter(context.Background(), fileID, dst)
	require.NoError(t, err)

	wrtr := NewWriter(context.Background(), testServer.URL+prefix, secret, fileID)

	src, err := randomFile(17_177_717)
	require.NoError(t, err)
	defer func() {
		_ = src.Close()
		_ = os.Remove(src.Name())
	}()

	n, err := io.CopyBuffer(wrtr, src, make([]byte, 1_577_777))
	require.NoError(t, err)
	assert.EqualValues(t, 17_177_717, n)

	_, err = dst.Seek(0, io.SeekStart)
	require.NoError(t, err)
	dstBuf, err := io.ReadAll(dst)
	require.NoError(t, err)

	_, err = src.Seek(0, io.SeekStart)
	require.NoError(t, err)
	srcBuf, err := io.ReadAll(src)
	require.NoError(t, err)

	assert.Equal(t, srcBuf, dstBuf)

	assert.NoError(t, wrtr.Close())
}

func TestPutRequest(t *testing.T) {
	srv := NewFileServer(prefix, secret)
	testServer := httptest.NewServer(srv)

	fileID, err := RandomFileID()
	require.NoError(t, err)
	dst, err := os.CreateTemp(os.TempDir(), "writer-put-test-")
	require.NoError(t, err)
	defer func() {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
	}()

	err = srv.ServeFileWriter(context.Background(), fileID, dst)
	require.NoError(t, err)

	wrtr := NewWriter(context.Background(), testServer.URL+prefix, secret, fileID)

	src, err := randomFile(133_799)
	require.NoError(t, err)
	defer func() {
		_ = src.Close()
		_ = os.Remove(src.Name())
	}()

	srcPath := src.Name()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, wrtr.PutURL(), src)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Reopen the rdr, because the http client closes it
	src, err = os.Open(srcPath)
	require.NoError(t, err)

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	_, err = dst.Seek(0, io.SeekStart)
	require.NoError(t, err)
	dstBuf, err := io.ReadAll(dst)
	require.NoError(t, err)

	_, err = src.Seek(0, io.SeekStart)
	require.NoError(t, err)
	srcBuf, err := io.ReadAll(src)
	require.NoError(t, err)

	assert.Equal(t, srcBuf, dstBuf)

	assert.NoError(t, wrtr.Close())
}
