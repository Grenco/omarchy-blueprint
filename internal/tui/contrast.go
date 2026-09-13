package tui

import (
	"math"
	"strconv"
	"strings"
)

func contrastRatio(first, second string) float64 {
	a, okA := rgb(first)
	b, okB := rgb(second)
	if !okA || !okB {
		return 0
	}
	lighter, darker := luminance(a), luminance(b)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + .05) / (darker + .05)
}

func rgb(value string) ([3]float64, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return [3]float64{}, false
	}
	result := [3]float64{}
	for i := range result {
		part, err := strconv.ParseUint(value[i*2:i*2+2], 16, 8)
		if err != nil {
			return [3]float64{}, false
		}
		result[i] = float64(part) / 255
	}
	return result, true
}
func luminance(rgb [3]float64) float64 {
	values := [3]float64{}
	for i, value := range rgb {
		if value <= .04045 {
			values[i] = value / 12.92
		} else {
			values[i] = math.Pow((value+.055)/1.055, 2.4)
		}
	}
	return .2126*values[0] + .7152*values[1] + .0722*values[2]
}
