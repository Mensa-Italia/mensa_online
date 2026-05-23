package generic

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
)

func RandomHash() string {
	h := sha256.New()
	h.Write([]byte(uuid.New().String() + "  " + uuid.New().String()))
	bs := h.Sum(nil)
	return hex.EncodeToString(bs)
}
