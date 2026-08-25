package handlerutils

import (
	"strconv"
	"strings"
)

func ParseIntParam(v string, d int) int {
	if strings.TrimSpace(v) == "" {
		return d
	}

	i, err := strconv.Atoi(v)
	if err != nil {
		return d
	}

	return i
}

// ClampIntParam parses v as an integer (returning d when blank or non-numeric)
// and clamps the result to [min, max]. Use this for query parameters like
// "limit" where unbounded values would cause excessive DB load.
func ClampIntParam(v string, d, min, max int) int {
	n := ParseIntParam(v, d)
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
