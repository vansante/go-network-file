package networkfile

import (
	"crypto/rand"
	"encoding/base64"
	"net/url"
)

// FileID is a unique descriptor for a file, should be usable in an URL.
type FileID string

// FileIDFromPath creates a usable FileID from a file path
func FileIDFromPath(path string) FileID {
	return FileID(url.PathEscape(path))
}

// RandomFileID returns a new random FileID
func RandomFileID() (FileID, error) {
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		return "", err
	}

	return FileID(base64.RawURLEncoding.EncodeToString(buf)), nil
}
