package aipower

import (
	"bytes"
	"context"
	"fmt"
	"github.com/fogleman/gg"
	"github.com/go-resty/resty/v2"
	"github.com/hbagdi/go-unsplash/unsplash"
	"github.com/tidwall/gjson"
	"golang.org/x/oauth2"
	"google.golang.org/genai"
	"image"
	"image/png"
	"log"
	"math"
	"math/rand/v2"
	"mensadb/tools/env"
)

func _generateEventImagePromptUplashQuery(prompt string) (string, error) {
	ctx := context.Background()
	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  env.GetGeminiKey(),
		Backend: genai.BackendGeminiAPI,
	})

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
				"query": &genai.Schema{
					Type: genai.TypeString,
				},
			},
		},
	}
	result, _ := client.Models.GenerateContent(
		ctx,
		"gemini-2.0-flash",
		genai.Text(fmt.Sprintf("-----\n%s\n\n----\n\nMake a search query for unsplash. You should use just a bunch words.", prompt)),
		config,
	)
	data := gjson.Parse(result.Text())
	promptToUse := data.Get("query").String()
	log.Println("Generated prompt for image search:", promptToUse)
	return promptToUse, nil
}

func _generateEventImageGenerationPrompt(prompt string) (string, error) {

	ctx := context.Background()
	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  env.GetGeminiKey(),
		Backend: genai.BackendGeminiAPI,
	})

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
				"prompt": &genai.Schema{
					Type: genai.TypeString,
				},
			},
		},
	}
	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.0-flash",
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

func _generateBackgroundImage(prompt string) ([]byte, error) {

	prompt, err := _generateEventImagePromptUplashQuery(prompt)
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: "Client-ID " + env.GetUnsplashKey()},
	)
	client := oauth2.NewClient(oauth2.NoContext, ts)
	//use the http.Client to instantiate unsplash
	unsplashClient := unsplash.New(client)
	// requests can be now made to the API
	searchedPhoto, _, err := unsplashClient.Search.Photos(&unsplash.SearchOpt{
		Query:   prompt,
		PerPage: 50,
	})
	if err != nil || searchedPhoto == nil || searchedPhoto.Results == nil || len(*searchedPhoto.Results) == 0 {
		return nil, fmt.Errorf("error searching for photos: %w", err)
	}

	randomIndex := 0 // You can randomize this if you want
	if len(*searchedPhoto.Results) > 1 {
		randomIndex = rand.IntN(len(*searchedPhoto.Results))
	}

	downloadPhoto, _ := resty.New().R().Get((*(searchedPhoto.Results))[randomIndex].Urls.Raw.String())
	return downloadPhoto.Body(), nil
}

func _generateBackgroundImageAI(prompt string) ([]byte, error) {
	ctx := context.Background()
	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  env.GetGeminiKey(),
		Backend: genai.BackendGeminiAPI,
	})

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
		"models/imagen-4.0-generate-preview-06-06",
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

// addText carica il font specificato, imposta il colore, l’allineamento e il debug,
// quindi disegna un testo in (x,y), contenuto in larghezza w e limitato a maxLines righe.
func addText(dc *gg.Context, fontPath string, fontSize float64, text string, x, y, w float64, maxLines int, align string, debug bool, r, g, b float64) *gg.Context {
	// Carica il font specificato
	if err := dc.LoadFontFace(fontPath, fontSize); err != nil {
		panic(err)
	}
	// Prepara colore testo
	dc.SetRGB(r, g, b)
	// Suddivide il testo in linee che non superano la larghezza w
	lines := dc.WordWrap(text, w)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	// Calcola interlinea e altezza totale del blocco
	lineGap := dc.FontHeight() * 1.2
	totalHeight := lineGap * float64(len(lines))
	// Debug: disegna un rettangolo intorno all’area di testo
	if debug {
		// usa rosso semitrasparente per il rettangolo
		dc.SetRGBA(1, 0, 0, 0.5)
		dc.SetLineWidth(1)
		dc.DrawRectangle(x, y, w, totalHeight)
		dc.Stroke()
		// ripristina colore testo
		dc.SetRGB(r, g, b)
	}
	// Determina posizione orizzontale in base all’allineamento
	var anchorX, textX float64
	switch align {
	case "center":
		anchorX = 0.5
		textX = x + w/2
	case "right":
		anchorX = 1
		textX = x + w
	default: // left
		anchorX = 0
		textX = x
	}
	// Disegna ogni linea
	for i, line := range lines {
		dy := y + float64(i+1)*lineGap
		dc.DrawStringAnchored(line, textX, dy, anchorX, 0)
	}
	return dc
}

// GenerateEventCard crea un’immagine combinata usando background.png, overlay.png e
// sei righe di testo, allineate a destra e centrate verticalmente.
func GenerateEventCard(title string, lines [5]string) ([]byte, error) {
	// Genera o carica sfondo da prompt
	descriptionPrompt := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s", title, lines[0], lines[1], lines[2], lines[3], lines[4])
	var bg image.Image
	bgBytes, err := _generateBackgroundImageAI(descriptionPrompt)
	if err != nil {
		return nil, err
	}
	// Decodifica generica (PNG, JPEG, ecc.) e converte in image.Image
	imgGeneric, _, err := image.Decode(bytes.NewReader(bgBytes))
	if err != nil {
		return nil, err
	}
	bg = imgGeneric
	// Carica overlay
	ov, err := gg.LoadImage("./pb_public/overlay.png")
	if err != nil {
		return nil, err
	}

	W := 1600
	H := 900
	dc := gg.NewContext(W, H)

	// Disegna sfondo coprendo il canvas senza distorsioni (cover)
	bw := float64(bg.Bounds().Dx())
	bh := float64(bg.Bounds().Dy())
	// Scala per coprire mantenendo l’aspect ratio
	scale := math.Max(float64(W)/bw, float64(H)/bh)
	nw := bw * scale
	nh := bh * scale
	// Centra l’immagine scalata
	dx := (float64(W) - nw) / 2
	dy := (float64(H) - nh) / 2
	dc.Push()
	dc.Translate(dx, dy)
	dc.Scale(scale, scale)
	dc.DrawImage(bg, 0, 0)
	dc.Pop()
	// Disegna overlay senza scala
	dc.DrawImage(ov, 0, 0)

	dc = addText(dc,
		"./pb_public/GothamBlack.ttf", 78,
		title,
		20, 400,
		(float64(W)/2)-70, 2,
		"right", false,
		1, 1, 1,
	)

	dc = addText(dc,
		"./pb_public/GothamMedium.ttf", 45,
		lines[0],
		20, 535,
		(float64(W)/2)-70, 2,
		"right", false,
		1, 1, 1,
	)

	dc = addText(dc,
		"./pb_public/GothamMedium.ttf", 45,
		lines[1],
		20, 580,
		(float64(W)/2)-70, 2,
		"right", false,
		1, 1, 1,
	)

	dc = addText(dc,
		"./pb_public/GothamMedium.ttf", 45,
		lines[2],
		20, 705,
		(float64(W)/2)-70, 2,
		"right", false,
		1, 1, 1,
	)
	dc = addText(dc,
		"./pb_public/GothamMedium.ttf", 32,
		lines[3],
		20, 760,
		(float64(W)/2)-70, 2,
		"right", false,
		1, 1, 1,
	)
	dc = addText(dc,
		"./pb_public/GothamMedium.ttf", 32,
		lines[4],
		20, 790,
		(float64(W)/2)-70, 2,
		"right", false,
		1, 1, 1,
	)

	// Esporta in PNG bytes
	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
