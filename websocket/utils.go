package websocket

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func generateClientID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("client_%d", randomInt())
	}
	return fmt.Sprintf("client_%s", hex.EncodeToString(bytes))
}

func randomInt() int64 {
	b := make([]byte, 8)
	rand.Read(b)
	var result int64
	for _, v := range b {
		result = (result << 8) | int64(v)
	}
	if result < 0 {
		result = -result
	}
	return result
}
