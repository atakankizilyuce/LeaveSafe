package qr

import (
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// Lines generates a QR code and returns it as a slice of strings (one per rendered row).
// Each string has equal visual width and contains only Unicode block/space characters.
func Lines(url string) ([]string, error) {
	qr, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("generate QR code: %w", err)
	}

	bitmap := qr.Bitmap()
	size := len(bitmap)

	var lines []string
	for y := 0; y < size; y += 2 {
		var row strings.Builder
		for x := 0; x < size; x++ {
			top := bitmap[y][x]
			bottom := false
			if y+1 < size {
				bottom = bitmap[y+1][x]
			}
			switch {
			case top && bottom:
				row.WriteString("\u2588")
			case top && !bottom:
				row.WriteString("\u2580")
			case !top && bottom:
				row.WriteString("\u2584")
			default:
				row.WriteString(" ")
			}
		}
		lines = append(lines, row.String())
	}
	return lines, nil
}

// PrintTerminal generates a QR code and prints it to the terminal.
func PrintTerminal(url string) error {
	lines, err := Lines(url)
	if err != nil {
		return err
	}
	fmt.Println()
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}
