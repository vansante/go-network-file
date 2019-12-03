package networkfile

import (
	"io"
	"sync"
)

type concurrentReadSeeker struct {
	src        io.ReadSeeker
	lastChange *readSeeker
	mu         sync.Mutex
}

func (rs *concurrentReadSeeker) New() io.ReadSeeker {
	return &readSeeker{
		parent: rs,
	}
}

type readSeeker struct {
	parent *concurrentReadSeeker
	offset int64
}

func (rs *readSeeker) Read(p []byte) (n int, err error) {
	rs.parent.mu.Lock()
	defer rs.parent.mu.Unlock()

	if rs.parent.lastChange != rs {
		n, err := rs.parent.src.Seek(rs.offset, io.SeekStart)
		rs.offset = n
		if err != nil {
			return 0, err
		}
	}
	rs.parent.lastChange = rs

	n, err = rs.parent.src.Read(p)
	rs.offset += int64(n)
	return n, err
}

func (rs *readSeeker) Seek(offset int64, whence int) (int64, error) {
	rs.parent.mu.Lock()
	defer rs.parent.mu.Unlock()

	rs.parent.lastChange = rs

	n, err := rs.parent.src.Seek(offset, whence)
	rs.offset = n
	return n, err
}
