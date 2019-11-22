package networkfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRandomSharedSecret(t *testing.T) {
	secret, err := RandomSharedSecret(10)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(secret), 10)

	secret, err = RandomSharedSecret(20)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(secret), 20)
}
