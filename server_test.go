package networkfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomSharedSecret(t *testing.T) {
	secret, err := RandomSharedSecret(10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(secret), 10)

	secret, err = RandomSharedSecret(20)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(secret), 20)
}
