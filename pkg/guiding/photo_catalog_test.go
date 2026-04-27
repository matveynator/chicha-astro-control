package guiding

import (
	"image"
	"image/color"
	"testing"
)

func TestIdentifyStarsFromPhotoDetectsCenterAndSurroundingStars(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 120, 90))
	fillFrame(frame, color.RGBA{R: 3, G: 3, B: 3, A: 255})

	placeStar(frame, 60, 45, 255)
	placeStar(frame, 28, 24, 230)
	placeStar(frame, 95, 67, 212)

	result, err := IdentifyStarsFromPhoto(frame, 5, 2)
	if err != nil {
		t.Fatalf("expected no identify error, got %v", err)
	}
	if result.DetectedCount < 3 {
		t.Fatalf("expected at least 3 stars, got %d", result.DetectedCount)
	}
	if int(result.CenterStar.X) != 60 || int(result.CenterStar.Y) != 45 {
		t.Fatalf("expected center star near 60x45, got %.1fx%.1f", result.CenterStar.X, result.CenterStar.Y)
	}
	if len(result.SurroundingStars) == 0 {
		t.Fatalf("expected surrounding stars")
	}
	if len(result.CenterStar.CatalogMatches) != 2 {
		t.Fatalf("expected exactly 2 catalog matches, got %d", len(result.CenterStar.CatalogMatches))
	}
}

func TestIdentifyStarsFromPhotoReturnsErrorWhenNoCandidatesFound(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 60, 40))
	fillFrame(frame, color.RGBA{R: 0, G: 0, B: 0, A: 255})

	_, err := IdentifyStarsFromPhoto(frame, 4, 2)
	if err == nil {
		t.Fatalf("expected error when no stars are visible")
	}
}

func fillFrame(frame *image.RGBA, fillColor color.RGBA) {
	for y := 0; y < frame.Bounds().Dy(); y += 1 {
		for x := 0; x < frame.Bounds().Dx(); x += 1 {
			frame.SetRGBA(x, y, fillColor)
		}
	}
}

func placeStar(frame *image.RGBA, centerX int, centerY int, peak uint8) {
	for offsetY := -1; offsetY <= 1; offsetY += 1 {
		for offsetX := -1; offsetX <= 1; offsetX += 1 {
			x := centerX + offsetX
			y := centerY + offsetY
			if x < 0 || y < 0 || x >= frame.Bounds().Dx() || y >= frame.Bounds().Dy() {
				continue
			}
			weight := uint8(0)
			if offsetX == 0 && offsetY == 0 {
				weight = peak
			} else {
				weight = peak / 2
			}
			frame.SetRGBA(x, y, color.RGBA{R: weight, G: weight, B: weight, A: 255})
		}
	}
}
