package aipower

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/google/generative-ai-go/genai"
	"github.com/tidwall/gjson"
	"google.golang.org/api/option"
	"image"
	"image/color"
	"image/png"
	"log"
	"mensadb/tools/env"
)

func GenerateStamp(prompt string) ([]byte, error) {
	ctx := context.Background()

	client, err := genai.NewClient(ctx, option.WithAPIKey(env.GetGeminiKey()))
	if err != nil {
		log.Fatalf("Error creating client: %v", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-2.0-flash")

	model.SetTemperature(1)
	model.SetTopK(40)
	model.SetTopP(0.95)
	model.SetMaxOutputTokens(8192)
	model.ResponseMIMEType = "application/json"
	model.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"prompt": &genai.Schema{
				Type: genai.TypeString,
			},
		},
	}

	session := model.StartChat()

	resp, err := session.SendMessage(ctx, genai.Text(fmt.Sprintf("Event Maker: Mensa Italia\n-----\n%s\n\n----\n\nMake a prompt to generate an image that is a circular stamp, black on white with a drawing that represent the event.\nDon't use names describe everything\nDefine what kind of text is need to be written on the outer ring, top and bottom, in Italian. include the event maker.\nBe descriptive", prompt)))
	if err != nil {
		log.Fatalf("Error sending message: %v", err)
	}

	for _, part := range resp.Candidates[0].Content.Parts {
		if textPart, ok := part.(genai.Text); ok {
			data := gjson.Parse(string(textPart))
			promptToUse := data.Get("prompt").String()
			return _generateStampImage(promptToUse)
		}
	}

	return nil, fmt.Errorf("No prompt found")
}
func _generateStampImage(prompt string) ([]byte, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash-exp-image-generation:generateContent?key=%s", env.GetGeminiKey())

	client := resty.New()

	response, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"parts": []map[string]interface{}{
						{"text": fmt.Sprintf(`Mensa Italia Event:
%s
---
Make a cricular stmap, black on white.`, prompt)},
					},
				},
			},
			"generationConfig": map[string]interface{}{
				"responseModalities": []string{"Text", "Image"},
			},
		}).
		Post(url)

	if err != nil {
		log.Fatal(err)
	}

	data := gjson.ParseBytes(response.Body())
	jsonResponse := data.Get("candidates.0.content.parts.0.inlineData.data").String()

	decodedData, err := base64.StdEncoding.DecodeString(jsonResponse)
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewReader(decodedData))
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	newImg := image.NewNRGBA(bounds)
	isImageWhite, _ := isImageMostlyWhite(img)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := img.At(x, y)
			r, g, b, _ := pixel.RGBA()
			brightness := (r + g + b) / 3 >> 8

			if isImageWhite {
				newImg.SetNRGBA(x, y, color.NRGBA{uint8(0), uint8(0), uint8(0), uint8(255 - brightness)})
			} else {
				newImg.SetNRGBA(x, y, color.NRGBA{uint8(0), uint8(0), uint8(0), uint8(brightness)})
			}
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, newImg); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func isImageMostlyWhite(img image.Image) (bool, error) {
	bounds := img.Bounds()
	whiteCount := 0
	blackCount := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			brightness := (r + g + b) / 3 >> 8
			if brightness > 127 {
				whiteCount++
			} else {
				blackCount++
			}
		}
	}

	return whiteCount > blackCount, nil
}
