package aipower

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"log"
	"math"
	"mensadb/tools/aitools"
	"time"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/tidwall/gjson"
	"golang.org/x/image/font"
	"google.golang.org/genai"
)

// Asset embeddati: overlay PNG e font TTF. Sono accanto al package perche`
// in produzione il binario gira con WORKDIR /pb/main mentre pb_public/ e`
// montato in /pb_public/, quindi i path relativi (./pb_public/...) erano
// fs.ErrNotExist - errore che PocketBase trasforma in 404 generico
// (tools/router/error.go ToApiError).
//
//go:embed assets/overlay.png
var overlayPNG []byte

//go:embed assets/GothamBlack.ttf
var gothamBlackTTF []byte

//go:embed assets/GothamMedium.ttf
var gothamMediumTTF []byte

var (
	parsedOverlay   image.Image
	parsedBlackFont *truetype.Font
	parsedMediumFont *truetype.Font
)

func init() {
	ov, _, err := image.Decode(bytes.NewReader(overlayPNG))
	if err != nil {
		log.Println("aipower: decode overlay embed:", err)
	} else {
		parsedOverlay = ov
	}
	if f, err := truetype.Parse(gothamBlackTTF); err != nil {
		log.Println("aipower: parse GothamBlack embed:", err)
	} else {
		parsedBlackFont = f
	}
	if f, err := truetype.Parse(gothamMediumTTF); err != nil {
		log.Println("aipower: parse GothamMedium embed:", err)
	} else {
		parsedMediumFont = f
	}
}

func faceFor(f *truetype.Font, points float64) font.Face {
	return truetype.NewFace(f, &truetype.Options{Size: points})
}

func _generateEventImageGenerationPrompt(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := aitools.GetAIClient()
	if client == nil {
		return "", fmt.Errorf("gemini client unavailable")
	}

	temp := float32(1)
	topP := float32(0.95)
	topK := float32(40.0)
	maxOutputTokens := int32(8192)

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		Temperature:      &temp,
		TopP:             &topP,
		TopK:             &topK,
		MaxOutputTokens:  maxOutputTokens,
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"prompt": {Type: genai.TypeString},
			},
		},
	}
	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-3-flash-preview",
		genai.Text(fmt.Sprintf("-----\n%s\n\n----\n\nUsing the previous data make a prompt to generate the best image that represents the event.\nUse a lot of details, be descriptive, use a lot of adjectives and nouns.\nUse the best words to describe the image you want to generate.", prompt)),
		config,
	)

	if err != nil {
		log.Println("Error generating event image prompt:", err)
		return "", err
	}
	data := gjson.Parse(result.Text())
	promptToUse := data.Get("prompt").String()

	log.Println("Generated prompt for image generation:", promptToUse)

	return promptToUse, nil
}

func _generateBackgroundImageAI(prompt string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client := aitools.GetAIClient()
	if client == nil {
		return nil, fmt.Errorf("gemini client unavailable")
	}

	promptToUse, err := _generateEventImageGenerationPrompt(prompt)
	if err != nil {
		log.Println("Error generating event image prompt:", err)
		return nil, err
	}
	config := &genai.GenerateImagesConfig{
		NumberOfImages:   1,
		OutputMIMEType:   "image/jpeg",
		PersonGeneration: genai.PersonGenerationAllowAdult,
		AspectRatio:      "16:9",
	}

	result, err := client.Models.GenerateImages(
		ctx,
		"models/imagen-4.0-generate-001",
		promptToUse,
		config,
	)

	if err != nil {
		log.Println("Response:", err.Error())
		return nil, fmt.Errorf("failed to generate image: %w", err)
	}
	var bytesOutput []byte
	for _, part := range result.GeneratedImages {
		if part.Image != nil {
			bytesOutput = part.Image.ImageBytes
			break
		}
	}
	if len(bytesOutput) == 0 {
		return nil, fmt.Errorf("imagen returned no image bytes")
	}

	img, _, err := image.Decode(bytes.NewReader(bytesOutput))
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

// addText disegna text in (x,y), contenuto in w e limitato a maxLines righe,
// usando il font.Face passato (gia` caricato dal font embed).
func addText(dc *gg.Context, face font.Face, text string, x, y, w float64, maxLines int, align string, debug bool, r, g, b float64) *gg.Context {
	dc.SetFontFace(face)
	dc.SetRGB(r, g, b)
	lines := dc.WordWrap(text, w)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	lineGap := dc.FontHeight() * 1.2
	totalHeight := lineGap * float64(len(lines))
	if debug {
		dc.SetRGBA(1, 0, 0, 0.5)
		dc.SetLineWidth(1)
		dc.DrawRectangle(x, y, w, totalHeight)
		dc.Stroke()
		dc.SetRGB(r, g, b)
	}
	var anchorX, textX float64
	switch align {
	case "center":
		anchorX = 0.5
		textX = x + w/2
	case "right":
		anchorX = 1
		textX = x + w
	default:
		anchorX = 0
		textX = x
	}
	for i, line := range lines {
		dy := y + float64(i+1)*lineGap
		dc.DrawStringAnchored(line, textX, dy, anchorX, 0)
	}
	return dc
}

// GenerateEventCard crea un'immagine combinata usando lo sfondo IA, l'overlay
// embeddato e sei righe di testo.
func GenerateEventCard(title string, lines [5]string) ([]byte, error) {
	if parsedOverlay == nil || parsedBlackFont == nil || parsedMediumFont == nil {
		return nil, fmt.Errorf("aipower: assets embed non disponibili (vedi log di init)")
	}

	descriptionPrompt := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s", title, lines[0], lines[1], lines[2], lines[3], lines[4])
	bgBytes, err := _generateBackgroundImageAI(descriptionPrompt)
	if err != nil {
		return nil, err
	}
	bg, _, err := image.Decode(bytes.NewReader(bgBytes))
	if err != nil {
		return nil, err
	}

	W := 1600
	H := 900
	dc := gg.NewContext(W, H)

	bw := float64(bg.Bounds().Dx())
	bh := float64(bg.Bounds().Dy())
	scale := math.Max(float64(W)/bw, float64(H)/bh)
	nw := bw * scale
	nh := bh * scale
	dx := (float64(W) - nw) / 2
	dy := (float64(H) - nh) / 2
	dc.Push()
	dc.Translate(dx, dy)
	dc.Scale(scale, scale)
	dc.DrawImage(bg, 0, 0)
	dc.Pop()

	dc.DrawImage(parsedOverlay, 0, 0)

	dc = addText(dc, faceFor(parsedBlackFont, 78), title,
		20, 400, (float64(W)/2)-70, 2, "right", false, 1, 1, 1)
	dc = addText(dc, faceFor(parsedMediumFont, 45), lines[0],
		20, 535, (float64(W)/2)-70, 2, "right", false, 1, 1, 1)
	dc = addText(dc, faceFor(parsedMediumFont, 45), lines[1],
		20, 580, (float64(W)/2)-70, 2, "right", false, 1, 1, 1)
	dc = addText(dc, faceFor(parsedMediumFont, 45), lines[2],
		20, 705, (float64(W)/2)-70, 2, "right", false, 1, 1, 1)
	dc = addText(dc, faceFor(parsedMediumFont, 32), lines[3],
		20, 760, (float64(W)/2)-70, 2, "right", false, 1, 1, 1)
	dc = addText(dc, faceFor(parsedMediumFont, 32), lines[4],
		20, 790, (float64(W)/2)-70, 2, "right", false, 1, 1, 1)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
