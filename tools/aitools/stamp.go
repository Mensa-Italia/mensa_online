package aitools

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"

	"github.com/tidwall/gjson"
	"google.golang.org/genai"
)

func GenerateStampPrompt(stampDescription string) string {
	ctx := context.Background()
	client := GetAIClient()

	config := &genai.GenerateContentConfig{
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingLevel: genai.ThinkingLevelHigh,
		},
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type:     genai.TypeObject,
			Required: []string{"json"},
			Properties: map[string]*genai.Schema{
				"json": &genai.Schema{
					Type: genai.TypeString,
				},
			},
		},
	}

	contents := []*genai.Content{
		{
			Role: genai.RoleUser,
			Parts: []*genai.Part{
				genai.NewPartFromText(stampDescription),
				genai.NewPartFromText("Generate a json description of a ink stamp based on the previous description. The json will be used to create an image, make it stylish, describe the internal image of the stamp and the text that goes in the upper and lower part of the stamp. Describe it as more realistic as possible, black and solid white background."),
			},
		},
	}

	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-3-flash-preview",
		contents,
		config,
	)
	if err != nil {
		return ""
	}

	return gjson.ParseBytes([]byte(result.Text())).Get("json").String()
}

func GenerateStamp(prompt string, makeitred bool) ([]byte, error) {
	newPrompt := GenerateStampPrompt(prompt)
	client := GetAIClient()

	images, err := client.Models.GenerateImages(
		context.Background(),
		"models/imagen-4.0-generate-001",
		"Make an ink stamp black and solid white background with the following description: "+newPrompt,
		&genai.GenerateImagesConfig{
			NumberOfImages:   1,
			OutputMIMEType:   "image/jpeg",
			PersonGeneration: genai.PersonGenerationAllowAdult,
			AspectRatio:      "1:1",
			ImageSize:        "1K",
		},
	)
	if err != nil {
		return nil, err
	}

	var bytesOutput []byte
	for _, part := range images.GeneratedImages {
		if part.Image != nil {
			bytesOutput = part.Image.ImageBytes
			break
		}
	}

	img, _, err := image.Decode(bytes.NewReader(bytesOutput))
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

			redVal := uint8(0)
			if makeitred {
				redVal = uint8(255)
			}
			if isImageWhite {
				newImg.SetNRGBA(x, y, color.NRGBA{redVal, uint8(0), uint8(0), uint8(255 - brightness)})
			} else {
				newImg.SetNRGBA(x, y, color.NRGBA{redVal, uint8(0), uint8(0), uint8(brightness)})
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
