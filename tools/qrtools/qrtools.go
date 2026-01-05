package qrtools

import (
	"bytes"
	"io"
	"log"

	"github.com/yeqown/go-qrcode/v2"
	"github.com/yeqown/go-qrcode/writer/standard"
	"github.com/yeqown/go-qrcode/writer/standard/shapes"
)

func HChainBlock(ctx *standard.DrawContext) {
	w, h := ctx.Edge()
	fw, fh := float64(w), float64(h)
	x, y := ctx.UpperLeft()
	cx, cy := x+fw/2, y+fh/2
	r := fw * 0.85 / 2 // todo:
	l := r * 0.2

	ctx.SetColor(ctx.Color())

	mask := ctx.Neighbours()

	drawRect := func(x, y, w, h float64) {
		ctx.DrawRectangle(x, y, w, h)
		ctx.Fill()
	}
	_ = mask
	_ = drawRect

	ctx.DrawCircle(cx, cy, r)

	if mask&standard.NLeft|standard.NSelf == standard.NLeft|standard.NSelf {
		drawRect(x, cy-l, fw/2, 2*l)
	}
	if mask&standard.NRight|standard.NSelf == standard.NRight|standard.NSelf {
		drawRect(cx, cy-l, fw/2, 2*l)
	}

	ctx.Fill()
}

// nopCloser è una struttura che implementa l'interfaccia io.Writer
// e fornisce un metodo Close() che non fa nulla, utile per scrivere
// dati in un buffer senza dover gestire la chiusura esplicita.
type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

func GenQrCode(content string) *bytes.Buffer {

	qrc, err := qrcode.NewWith(content,
		qrcode.WithEncodingMode(qrcode.EncModeByte),
		qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionHighest),
	)

	if err != nil {
		// Log dell'errore nella generazione del QRCode
		log.Printf("Errore nella generazione del QRCode: %v", err)
		return nil
	}
	shape := shapes.Assemble(shapes.RoundedFinder(), shapes.LiquidBlock())
	var data uint8 = 32
	options := []standard.ImageOption{
		standard.WithQRWidth(data),
		standard.WithLogoImageFileJPEG("../pb_public/test-2.jpg"),
		standard.WithLogoSizeMultiplier(1),
		standard.WithLogoSafeZone(),
		standard.WithCustomShape(shape),
		standard.WithBorderWidth(20),
	}

	// Creazione dell'immagine del timbro da inviare via email
	stampImage := bytes.NewBuffer(nil)
	wr := nopCloser{Writer: stampImage}
	w2 := standard.NewWithWriter(wr, options...)
	defer w2.Close()
	if err = qrc.Save(w2); err != nil {
		log.Printf("Errore nel salvataggio del QRCode: %v", err)
		return nil
	}
	return stampImage
}
