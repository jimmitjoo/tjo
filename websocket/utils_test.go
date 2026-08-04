package websocket

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateClientID(t *testing.T) {
	id1 := generateClientID()
	id2 := generateClientID()

	assert.True(t, strings.HasPrefix(id1, "client_"))
	assert.True(t, strings.HasPrefix(id2, "client_"))
	assert.NotEqual(t, id1, id2)
	assert.True(t, len(id1) > 7)
}

func TestRandomInt(t *testing.T) {
	num1 := randomInt()
	num2 := randomInt()

	assert.True(t, num1 >= 0)
	assert.True(t, num2 >= 0)

	allSame := true
	for i := 0; i < 10; i++ {
		if randomInt() != num1 {
			allSame = false
			break
		}
	}
	assert.False(t, allSame, "Random function should generate different values")
}
