package aipower

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
	"google.golang.org/genai"
	"image"
	"image/color"
	"image/png"
	"mensadb/tools/env"
)

func GenerateStamp(prompt string) ([]byte, error) {
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
	result, _ := client.Models.GenerateContent(
		ctx,
		"gemini-2.0-flash",
		genai.Text(fmt.Sprintf("Event Maker: Mensa Italia\n-----\n%s\n\n----\n\nMake a prompt to generate an image that is a circular stamp, black on white with a drawing that represent the event.\nDon't use names describe everything\nDefine what kind of text is need to be written on the outer ring, top and bottom, in Italian. include the event maker.\nBe descriptive", prompt)),
		config,
	)
	data := gjson.Parse(result.Text())
	promptToUse := data.Get("prompt").String()
	return _generateStampImage(promptToUse)
}
func _generateStampImage(prompt string) ([]byte, error) {

	url := "https://ir-api.myqa.cc/v1/openai/images/generations"

	response, err := resty.New().R().
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", env.GetImageRouterKey())).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]interface{}{
			"prompt":  prompt,
			"model":   "google/gemini-2.0-flash-exp:free",
			"quality": "auto",
		}).
		Post(url)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	data := gjson.ParseBytes(response.Body())
	jsonResponse := data.Get("data.0.b64_json").String()

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
