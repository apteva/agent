package tools

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"time"
)

type GenerateTestImageTool struct{}

func (t *GenerateTestImageTool) Name() string {
	return "generate_test_image"
}

func (t *GenerateTestImageTool) DisplayName() string {
	return "Generate Test Image"
}

func (t *GenerateTestImageTool) Description() string {
	return "Generates a test image as base64 for testing file storage. Default: 800x600 random pattern (~600KB base64) to ensure extraction. Patterns: random (large), checkerboard, gradient, solid (small)."
}

func (t *GenerateTestImageTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Pattern to generate. Use 'random' for large files (best for testing extraction). Gradient/solid compress very small.",
				"enum":        []string{"random", "checkerboard", "gradient", "solid"},
				"default":     "random",
			},
			"width": map[string]interface{}{
				"type":        "integer",
				"description": "Image width in pixels",
				"minimum":     100,
				"maximum":     2000,
				"default":     800,
			},
			"height": map[string]interface{}{
				"type":        "integer",
				"description": "Image height in pixels",
				"minimum":     100,
				"maximum":     2000,
				"default":     600,
			},
			"color": map[string]interface{}{
				"type":        "string",
				"description": "Color for solid pattern: red, green, blue, purple",
				"default":     "blue",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *GenerateTestImageTool) Execute(params map[string]interface{}) (interface{}, error) {
	// Set defaults - use random pattern and larger size to ensure base64 > 10KB for extraction
	width := 800
	height := 600
	pattern := "random" // Random pattern doesn't compress well, generating larger base64
	colorName := "blue"

	// Parse parameters
	if w, ok := params["width"].(float64); ok {
		width = int(w)
	}
	if h, ok := params["height"].(float64); ok {
		height = int(h)
	}
	if p, ok := params["pattern"].(string); ok {
		pattern = p
	}
	if c, ok := params["color"].(string); ok {
		colorName = c
	}

	// Validate
	if width < 100 || width > 2000 {
		return nil, fmt.Errorf("width must be between 100 and 2000")
	}
	if height < 100 || height > 2000 {
		return nil, fmt.Errorf("height must be between 100 and 2000")
	}

	// Generate image
	img := generateImage(width, height, pattern, colorName)

	// Encode to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode PNG: %w", err)
	}

	// Convert to base64
	base64Str := base64.StdEncoding.EncodeToString(buf.Bytes())
	sizeKB := float64(len(base64Str)) / 1024.0

	// Return as nested structure to test ExtractImagesFromToolResult
	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Generated %dx%d %s test image (%.1f KB base64)", width, height, pattern, sizeKB),
		"data": map[string]interface{}{
			"metadata": map[string]interface{}{
				"width":      width,
				"height":     height,
				"pattern":    pattern,
				"size_bytes": buf.Len(),
			},
			"images": []interface{}{
				map[string]interface{}{
					"data":     base64Str,
					"mimeType": "image/png",
					"format":   "base64",
				},
			},
		},
	}, nil
}

func generateImage(width, height int, pattern, colorName string) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	switch pattern {
	case "solid":
		fillSolid(img, getColor(colorName))
	case "gradient":
		fillGradient(img)
	case "checkerboard":
		fillCheckerboard(img)
	case "random":
		fillRandom(img)
	default:
		fillGradient(img)
	}

	return img
}

func getColor(name string) color.RGBA {
	colors := map[string]color.RGBA{
		"red":    {255, 0, 0, 255},
		"green":  {0, 255, 0, 255},
		"blue":   {0, 0, 255, 255},
		"purple": {128, 0, 128, 255},
	}
	if c, ok := colors[name]; ok {
		return c
	}
	return colors["blue"]
}

func fillSolid(img *image.RGBA, c color.RGBA) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.Set(x, y, c)
		}
	}
}

func fillGradient(img *image.RGBA) {
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// Create a diagonal gradient
			r := uint8(float64(x) / float64(width) * 255)
			g := uint8(float64(y) / float64(height) * 255)
			b := uint8(128)
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
}

func fillCheckerboard(img *image.RGBA) {
	bounds := img.Bounds()
	squareSize := 50

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			isEven := ((x/squareSize)+(y/squareSize))%2 == 0
			if isEven {
				img.Set(x, y, color.RGBA{255, 255, 255, 255}) // White
			} else {
				img.Set(x, y, color.RGBA{0, 0, 0, 255}) // Black
			}
		}
	}
}

func fillRandom(img *image.RGBA) {
	bounds := img.Bounds()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r := uint8(rng.Intn(256))
			g := uint8(rng.Intn(256))
			b := uint8(rng.Intn(256))
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
}
