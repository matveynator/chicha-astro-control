package guiding

import (
	"errors"
	"image"
	"math"
	"sort"
)

const (
	defaultPhotoSearchStarLimit     = 6
	defaultCatalogMatchesPerStar    = 3
	minimumPhotoSearchStarLimit     = 3
	maximumPhotoSearchStarLimit     = 12
	minimumCatalogMatchesPerStar    = 1
	maximumCatalogMatchesPerStar    = 5
	minimumPhotoCandidateSeparation = 8.0
)

type CatalogMatch struct {
	Name            string  `json:"name"`
	Constellation   string  `json:"constellation"`
	VisualMagnitude float64 `json:"visual_magnitude"`
	MagnitudeDelta  float64 `json:"magnitude_delta"`
}

type DetectedPhotoStar struct {
	X                float64        `json:"x"`
	Y                float64        `json:"y"`
	Brightness       float64        `json:"brightness"`
	DistanceToCenter float64        `json:"distance_to_center"`
	CatalogMatches   []CatalogMatch `json:"catalog_matches"`
}

type PhotoCatalogResult struct {
	FrameWidth       int                 `json:"frame_width"`
	FrameHeight      int                 `json:"frame_height"`
	DetectedCount    int                 `json:"detected_count"`
	CenterStar       DetectedPhotoStar   `json:"center_star"`
	SurroundingStars []DetectedPhotoStar `json:"surrounding_stars"`
}

type rawPhotoStar struct {
	x          int
	y          int
	brightness float64
}

func IdentifyStarsFromPhoto(frame image.Image, maxStars int, maxCatalogMatches int) (PhotoCatalogResult, error) {
	bounds := frame.Bounds()
	frameWidth := bounds.Dx()
	frameHeight := bounds.Dy()
	if frameWidth < 3 || frameHeight < 3 {
		return PhotoCatalogResult{}, errors.New("frame is too small for star detection")
	}

	maxStars = clampInt(maxStars, minimumPhotoSearchStarLimit, maximumPhotoSearchStarLimit)
	if maxStars == 0 {
		maxStars = defaultPhotoSearchStarLimit
	}
	maxCatalogMatches = clampInt(maxCatalogMatches, minimumCatalogMatchesPerStar, maximumCatalogMatchesPerStar)
	if maxCatalogMatches == 0 {
		maxCatalogMatches = defaultCatalogMatchesPerStar
	}

	meanBrightness, standardDeviation := computeBrightnessStats(frame, bounds)
	minimumBrightness := meanBrightness + (1.2 * standardDeviation)
	if minimumBrightness < 24 {
		minimumBrightness = 24
	}

	rawCandidates := detectLocalPeakCandidates(frame, bounds, minimumBrightness)
	selectedCandidates := selectBrightCandidates(rawCandidates, maxStars, frameWidth, frameHeight)
	if len(selectedCandidates) == 0 {
		return PhotoCatalogResult{}, errors.New("no stars found in frame")
	}

	frameCenterX := float64(frameWidth-1) / 2
	frameCenterY := float64(frameHeight-1) / 2
	detectedStars := make([]DetectedPhotoStar, 0, len(selectedCandidates))
	for _, candidate := range selectedCandidates {
		distanceToCenter := math.Hypot(float64(candidate.x)-frameCenterX, float64(candidate.y)-frameCenterY)
		detectedStars = append(detectedStars, DetectedPhotoStar{
			X:                float64(candidate.x),
			Y:                float64(candidate.y),
			Brightness:       candidate.brightness,
			DistanceToCenter: distanceToCenter,
			CatalogMatches:   findCatalogMatchesByBrightness(candidate.brightness, maxCatalogMatches),
		})
	}

	sort.Slice(detectedStars, func(leftIndex int, rightIndex int) bool {
		return detectedStars[leftIndex].DistanceToCenter < detectedStars[rightIndex].DistanceToCenter
	})

	return PhotoCatalogResult{
		FrameWidth:       frameWidth,
		FrameHeight:      frameHeight,
		DetectedCount:    len(detectedStars),
		CenterStar:       detectedStars[0],
		SurroundingStars: detectedStars[1:],
	}, nil
}

func computeBrightnessStats(frame image.Image, bounds image.Rectangle) (float64, float64) {
	pixelCount := float64(bounds.Dx() * bounds.Dy())
	if pixelCount == 0 {
		return 0, 0
	}

	sum := 0.0
	sumSquares := 0.0
	for pixelY := 0; pixelY < bounds.Dy(); pixelY += 1 {
		for pixelX := 0; pixelX < bounds.Dx(); pixelX += 1 {
			brightness := grayBrightness(frame.At(pixelX+bounds.Min.X, pixelY+bounds.Min.Y))
			sum += brightness
			sumSquares += brightness * brightness
		}
	}
	mean := sum / pixelCount
	variance := (sumSquares / pixelCount) - (mean * mean)
	if variance < 0 {
		variance = 0
	}
	return mean, math.Sqrt(variance)
}

func detectLocalPeakCandidates(frame image.Image, bounds image.Rectangle, minimumBrightness float64) []rawPhotoStar {
	candidates := make([]rawPhotoStar, 0, 128)
	for pixelY := 1; pixelY < bounds.Dy()-1; pixelY += 1 {
		for pixelX := 1; pixelX < bounds.Dx()-1; pixelX += 1 {
			centerBrightness := grayBrightness(frame.At(pixelX+bounds.Min.X, pixelY+bounds.Min.Y))
			if centerBrightness < minimumBrightness {
				continue
			}
			if !isLocalBrightnessPeak(frame, bounds, pixelX, pixelY, centerBrightness) {
				continue
			}
			candidates = append(candidates, rawPhotoStar{x: pixelX, y: pixelY, brightness: centerBrightness})
		}
	}
	return candidates
}

func isLocalBrightnessPeak(frame image.Image, bounds image.Rectangle, centerX int, centerY int, centerBrightness float64) bool {
	for offsetY := -1; offsetY <= 1; offsetY += 1 {
		for offsetX := -1; offsetX <= 1; offsetX += 1 {
			if offsetX == 0 && offsetY == 0 {
				continue
			}
			neighborBrightness := grayBrightness(frame.At(centerX+offsetX+bounds.Min.X, centerY+offsetY+bounds.Min.Y))
			if neighborBrightness > centerBrightness {
				return false
			}
		}
	}
	return true
}

func selectBrightCandidates(candidates []rawPhotoStar, maxStars int, frameWidth int, frameHeight int) []rawPhotoStar {
	sort.Slice(candidates, func(leftIndex int, rightIndex int) bool {
		return candidates[leftIndex].brightness > candidates[rightIndex].brightness
	})

	minimumSeparation := minimumPhotoCandidateSeparation
	if shorterSide := math.Min(float64(frameWidth), float64(frameHeight)); shorterSide > 0 {
		dynamicSeparation := shorterSide / 28
		if dynamicSeparation > minimumSeparation {
			minimumSeparation = dynamicSeparation
		}
	}

	selected := make([]rawPhotoStar, 0, maxStars)
	for _, candidate := range candidates {
		if len(selected) >= maxStars {
			break
		}
		isTooCloseToAnotherStar := false
		for _, acceptedCandidate := range selected {
			distance := math.Hypot(float64(candidate.x-acceptedCandidate.x), float64(candidate.y-acceptedCandidate.y))
			if distance < minimumSeparation {
				isTooCloseToAnotherStar = true
				break
			}
		}
		if isTooCloseToAnotherStar {
			continue
		}
		selected = append(selected, candidate)
	}

	return selected
}

func findCatalogMatchesByBrightness(starBrightness float64, maxMatches int) []CatalogMatch {
	if len(brightStarCatalog) == 0 {
		return nil
	}
	maxMatches = clampInt(maxMatches, minimumCatalogMatchesPerStar, maximumCatalogMatchesPerStar)
	if maxMatches == 0 {
		maxMatches = defaultCatalogMatchesPerStar
	}

	estimatedMagnitude := estimateVisualMagnitude(starBrightness)
	matches := make([]CatalogMatch, 0, len(brightStarCatalog))
	for _, starEntry := range brightStarCatalog {
		matches = append(matches, CatalogMatch{
			Name:            starEntry.Name,
			Constellation:   starEntry.Constellation,
			VisualMagnitude: starEntry.VisualMagnitude,
			MagnitudeDelta:  math.Abs(starEntry.VisualMagnitude - estimatedMagnitude),
		})
	}

	sort.Slice(matches, func(leftIndex int, rightIndex int) bool {
		return matches[leftIndex].MagnitudeDelta < matches[rightIndex].MagnitudeDelta
	})
	if maxMatches > len(matches) {
		maxMatches = len(matches)
	}
	return matches[:maxMatches]
}

func estimateVisualMagnitude(starBrightness float64) float64 {
	clampedBrightness := clampFloat64(starBrightness, 0, 255)
	brightnessRatio := clampedBrightness / 255
	return 3.5 - (brightnessRatio * 5.0)
}
