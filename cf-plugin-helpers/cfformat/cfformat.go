package cfformat

import "fmt"

// ByteSize returns a human-readable byte size string.
// This is a drop-in replacement for cf/formatters.ByteSize().
func ByteSize(bytes int64) string {
	const (
		_          = iota
		kb float64 = 1 << (10 * iota)
		mb
		gb
		tb
	)

	unit := ""
	value := float64(bytes)

	switch {
	case value >= tb:
		unit = "T"
		value /= tb
	case value >= gb:
		unit = "G"
		value /= gb
	case value >= mb:
		unit = "M"
		value /= mb
	case value >= kb:
		unit = "K"
		value /= kb
	default:
		return fmt.Sprintf("%dB", bytes)
	}

	return fmt.Sprintf("%.1f%s", value, unit)
}
