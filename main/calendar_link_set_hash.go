package main

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
)

func CalendarSetHash(e *core.RecordEvent) error {
	e.Record.Set("hash", randomHash())
	_ = app.Save(e.Record)
	return e.Next()
}

func randomHash() string {
	h := sha256.New()
	h.Write([]byte(uuid.New().String() + "  " + uuid.New().String()))
	bs := h.Sum(nil)
	return hex.EncodeToString(bs)
}
