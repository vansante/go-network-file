package networkfile

import (
	"os"
	"time"
)

// Statter is an interface for files which implement statting
type Statter interface {
	Stat() (os.FileInfo, error)
}

// FileInfo is used to send stat requests over HTTP
type FileInfo struct {
	FileName    string      `json:"name"`    // base name of the file
	FileSize    int64       `json:"size"`    // length in bytes for regular files; system-dependent for others
	FileModTime int64       `json:"modtime"` // modification time
	FileMode    os.FileMode `json:"mode"`    // file mode bits
	FileIsDir   bool        `json:"isdir"`   // abbreviation for Mode().IsDir()
}

// GetFileInfo returns a FileInfo struct from the os.FileInfo interface
func GetFileInfo(fi os.FileInfo) FileInfo {
	return FileInfo{
		FileName:    fi.Name(),
		FileSize:    fi.Size(),
		FileMode:    fi.Mode(),
		FileModTime: fi.ModTime().UnixNano(),
		FileIsDir:   fi.IsDir(),
	}
}

// Name returns the filename
func (f *FileInfo) Name() string {
	return f.FileName
}

// Size returns the file size
func (f *FileInfo) Size() int64 {
	return f.FileSize
}

// Mode returns the file mode
func (f *FileInfo) Mode() os.FileMode {
	return f.FileMode
}

// ModTime returns the file modified time
func (f *FileInfo) ModTime() time.Time {
	return time.Unix(0, f.FileModTime)
}

// IsDir returns whether the file is a dir
func (f *FileInfo) IsDir() bool {
	return f.FileIsDir
}

// Sys is not implemented
func (f *FileInfo) Sys() interface{} {
	return nil
}
