package main

import "unicode"

// columnWidth covers combining marks and the wide CJK/fullwidth characters used
// by the supported languages. ANSI escapes are removed by the caller. Terminal
// shaping and bidi rendering are provided by the terminal, not by the daemon.
func columnWidth(r rune) int {
	if r < 32 || (r >= 0x7f && r < 0xa0) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if (r >= 0x1100 && r <= 0x115f) || (r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) || (r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff01 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) || (r >= 0x20000 && r <= 0x3fffd) {
		return 2
	}
	return 1
}
