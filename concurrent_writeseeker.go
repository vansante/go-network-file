package networkfile

import (
	"io"
	"sync"
)

type concurrentWriteSeeker struct {
	src        io.WriteSeeker
	lastChange *writeSeeker
	mu         sync.Mutex
}

func (rs *concurrentWriteSeeker) New() io.WriteSeeker {
	return &writeSeeker{
		parent: rs,
	}
}

type writeSeeker struct {
	parent *concurrentWriteSeeker
	offset int64
}

func (rs *writeSeeker) Write(p []byte) (n int, err error) {
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

	n, err = rs.parent.src.Write(p)
	rs.offset += int64(n)
	return n, err
}

func (rs *writeSeeker) Seek(offset int64, whence int) (int64, error) {
	rs.parent.mu.Lock()
	defer rs.parent.mu.Unlock()

	rs.parent.lastChange = rs

	n, err := rs.parent.src.Seek(offset, whence)
	rs.offset = n
	return n, err
}
