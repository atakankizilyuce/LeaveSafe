package main

import (
	"strings"
	"unicode/utf8"
)

// What the parts of the dashboard that never move take up, and the room the
// rest of it is allowed.
const (
	// qrIndent is the left margin the QR box is drawn at.
	qrIndent = 2
	// gap is the space between the QR box and the status grid beside it.
	gap = 3
	// bannerRows is what drawHeader draws before anything else: six lines of
	// block letters, the version and a rule.
	bannerRows = 8
	// bannerWidth is how wide the block letters are, indent included. Narrower
	// than this they are dropped for a single line: they do not wrap
	// gracefully, and a header that folds onto twelve rows puts everything the
	// layout placed below it on top of something else.
	bannerWidth = 77
	// shortBannerRows is that single line and the rule under it.
	shortBannerRows = 2
	// scanLabelRows is the "Scan to connect:" line above the code.
	scanLabelRows = 1
	// footerRows is the blank line, the command list and the rule under them.
	footerRows = 3
	// minLogRows is how much of the window is kept for the log. It is the part
	// that says what is happening, so it is reserved before anything else is
	// allowed to grow into it.
	minLogRows = 3

	// The status grid is clamped at both ends. Narrower than this its lines
	// wrap, and a wrapped line in a layout drawn at absolute positions pushes
	// everything below it down while the next repaint puts it back — which is
	// how two status grids ended up on one screen. Wider and it sprawls across
	// a maximized window with a yard of space in the middle.
	minGridWidth = 34
	maxGridWidth = 50
	// floorGridWidth is the narrowest box that can still be drawn at all. A
	// window this small gets something misshapen rather than a panic.
	floorGridWidth = 12
)

// layout is where everything the dashboard draws goes on screen, worked out
// from the size of the window and the size of what has to fit in it.
//
// It is one value rather than a handful of fields on the status bar because
// the dashboard is repainted in pieces, and a piece drawn against a layout the
// screen no longer has is exactly how two status grids ended up on one screen:
// the window was resized, the grid moved, and the piece was painted at the rows
// the old one occupied. Holding the layout the screen was
// drawn with makes that comparable — see statusBar.layoutHolds.
type layout struct {
	termW, termH int

	// headRows is how many rows the header takes, which the header itself
	// cannot work out: it depends on how much room the code needed, and a
	// header that drew a different number of rows than the layout allowed for
	// put its own banner under everything placed below it.
	headRows int

	gridRow, gridCol, gridWidth int
	// gridRows is how many of the grid's rows there is room to draw. It is the
	// whole grid in any window worth using, and fewer in one too small to hold
	// it — where the choice is between a clipped grid and a grid drawn into the
	// rows the log scrolls, which would scroll it away a line at a time.
	gridRows int

	// The QR box. A height of zero means there was no room for a code in this
	// window, and the addresses listed in the grid are what the user is left
	// with.
	qrRow, qrCol, qrBoxW, qrBoxH int

	// labelRow is where the line above the code goes — the invitation to scan
	// it, or the note that says why there is none.
	labelRow int

	// footerShown is false in a window with no room for both the status grid
	// and the command list under it. The footer is drawn at fixed rows above
	// the log, so a grid that reached them was drawn over — the address to pair
	// with, with a list of commands through the middle of it.
	footerShown bool

	// logRow is the first row of the scrolling region: everything above it is
	// drawn once and must stay put, everything from it down scrolls.
	logRow int
}

// room is a window, everything that has to go in it, and what this attempt is
// allowed to leave out.
//
// It is one value rather than seven parameters threaded through every shape
// below, which is what it was before the banner and the footer both became
// things that could give way.
type room struct {
	termW, termH int
	// qrW, qrH is the code to place. A height of zero means there is none —
	// either none was rendered, or this attempt is the one that gave it up.
	qrW, qrH int
	gridH    int
	// head and foot are the rows this attempt allows the banner and the command
	// list. Both go to nothing before the code does.
	head, foot int
}

// computeLayout places the QR box, the status grid and the scrolling region in
// a window of the given size.
//
// Three shapes, in order of preference. Side by side is the one the dashboard
// was designed around. A window too narrow for both puts the code above the
// grid instead, which is worth more than a grid drawn over the code. A window
// with room for neither drops the code — it is the one part that cannot be made
// smaller, and the addresses it encodes are in the grid as text anyway.
func computeLayout(termW, termH, qrW, qrH, gridH int) layout {
	r := room{termW: termW, termH: termH, qrW: qrW, qrH: qrH, gridH: gridH}

	// In priority order, and the order is the point: everything that is there
	// to be read gives way before the one thing that is there to be scanned.
	// The block letters go first, then the command list — `help` says all of
	// that again — then the shape changes, and only when none of it was enough
	// does the code go.
	//
	// This matters at ordinary sizes, not exotic ones. A code for an address
	// with a pairing key in it is nineteen rows, and a default terminal window
	// is thirty; the banner, the label, the footer and the log's own three rows
	// take the rest, so keeping all of them meant reporting no room for the
	// code in a window that plainly had some.
	for _, foot := range []int{footerRows, 0} {
		for _, head := range []int{bannerHeight(termW), shortBannerRows} {
			for _, place := range []placement{placeSideBySide, placeStacked} {
				r.head, r.foot = head, foot
				if l, ok := place(r); ok {
					return l
				}
			}
		}
	}

	r.head, r.foot = bannerHeight(termW), footerRows
	return placeWithoutCode(r)
}

// placement is one of the shapes the dashboard can take, which reports whether
// it fits in the window it was asked about.
type placement func(r room) (layout, bool)

// placeSideBySide puts the code on the left with the status grid beside it,
// which is the shape the dashboard was designed around.
func placeSideBySide(r room) (layout, bool) {
	l, blockRow := newLayout(r)
	if r.qrH == 0 || qrIndent+r.qrW+gap+minGridWidth > r.termW {
		return l, false
	}

	block := max(r.qrH, r.gridH)
	if blockRow+block+r.foot > lastDrawableRow(r.termH) {
		return l, false
	}

	l.qrCol = qrIndent + 1
	l.qrBoxW, l.qrBoxH = r.qrW, r.qrH
	l.gridCol = qrIndent + r.qrW + gap + 1
	l.gridWidth = clampGridWidth(r.termW - l.gridCol)

	// Each column is centered against the taller of the two. In practice the
	// code is taller, but a laptop with every sensor listed and a small code is
	// the other way round, and drawing the code hard against the top with a
	// column of blank beneath it looks like a mistake.
	l.qrRow = blockRow + (block-r.qrH)/2
	l.gridRow = blockRow + (block-r.gridH)/2
	l.logRow = blockRow + block + r.foot
	l.gridRows = r.gridH
	return l, true
}

// placeStacked puts the code above the grid, for a window too narrow to hold
// them side by side. Worth more than a grid drawn over the code.
func placeStacked(r room) (layout, bool) {
	l, blockRow := newLayout(r)
	if r.qrH == 0 || qrIndent+r.qrW > r.termW {
		return l, false
	}
	if blockRow+r.qrH+1+r.gridH+r.foot > lastDrawableRow(r.termH) {
		return l, false
	}

	l.qrCol = qrIndent + 1
	l.qrRow = blockRow
	l.qrBoxW, l.qrBoxH = r.qrW, r.qrH
	l.gridCol = qrIndent + 1
	l.gridWidth = clampGridWidth(r.termW - l.gridCol)
	l.gridRow = blockRow + r.qrH + 1
	l.logRow = l.gridRow + r.gridH + r.foot
	l.gridRows = r.gridH
	return l, true
}

// placeWithoutCode drops the QR code, which is what a window with room for
// neither shape gets.
//
// A code cannot be made smaller — it is as many modules as the address it
// carries — so this is the end of the line rather than a further compromise.
// The addresses it would have encoded are listed in the grid as text, and the
// label above says so.
func placeWithoutCode(r room) layout {
	l, blockRow := newLayout(r)
	l.gridCol = qrIndent + 1
	l.gridWidth = clampGridWidth(r.termW - l.gridCol)
	l.gridRow = blockRow

	// The footer goes here too, for the grid this time. It is drawn at fixed
	// rows above the log, so a grid that reached them was drawn over: the
	// address to pair with, with a list of commands through the middle of it.
	// Taking rows off the grid instead would be keeping the reminder and losing
	// the thing it reminds you about.
	foot := r.foot
	if blockRow+r.gridH+foot > lastDrawableRow(r.termH) {
		foot = 0
	}
	l.footerShown = foot > 0

	l.logRow = min(blockRow+r.gridH+foot, lastDrawableRow(r.termH)+1)
	// Clipped rather than allowed to run into the log. A window this small
	// cannot show the whole grid, and drawing the rest of it into the scrolling
	// region would not show it either — it would scroll away a line at a time
	// while the layout went on believing it was there.
	l.gridRows = max(min(r.gridH, l.logRow-foot-l.gridRow), 0)
	return l
}

// newLayout starts a layout with the parts every shape shares, and hands back
// the row the code and the grid begin on.
func newLayout(r room) (layout, int) {
	blockRow := r.head + scanLabelRows + 2
	return layout{
		termW:       r.termW,
		termH:       r.termH,
		headRows:    r.head,
		labelRow:    blockRow - 1,
		logRow:      blockRow,
		footerShown: r.foot > 0,
	}, blockRow
}

// lastDrawableRow is the lowest row anything drawn once may occupy: below it
// are the rows kept for the log.
func lastDrawableRow(termH int) int {
	return max(termH-minLogRows, 1)
}

// bannerHeight is how many rows the header takes in a window this wide.
func bannerHeight(termW int) int {
	if termW < bannerWidth {
		return shortBannerRows
	}
	return bannerRows
}

// clampGridWidth keeps the status grid inside the window and inside the width
// its lines are written for.
func clampGridWidth(available int) int {
	return max(min(available, maxGridWidth), floorGridWidth)
}

// ellipsize shortens a plain string to fit, and marks that it was cut.
//
// The mark matters here because what gets shortened is an address: one silently
// missing its last few characters is one the user copies down wrong, and finds
// out about it on a phone that will not connect.
func ellipsize(s string, maxVisible int) string {
	if maxVisible <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxVisible {
		return s
	}
	if maxVisible <= 3 {
		return strings.Repeat(".", maxVisible)
	}
	return string([]rune(s)[:maxVisible-3]) + "..."
}

// truncateVisible cuts a string to a number of visible characters, leaving the
// color escapes in it alone.
//
// The escapes are zero-width, so counting bytes or runes overstates the length
// of every line the dashboard draws — and a line the layout believed fit but
// which does not wraps, pushing everything below it down a row while the next
// repaint draws it back where it was. A color is closed off at the cut, or the
// rest of the row would take it.
func truncateVisible(s string, maxVisible int) string {
	if maxVisible < 0 {
		maxVisible = 0
	}

	var b strings.Builder
	seen, colored := 0, false
	for i := 0; i < len(s); {
		if n := escapeLen(s, i); n > 0 {
			b.WriteString(s[i : i+n])
			colored = true
			i += n
			continue
		}
		if seen == maxVisible {
			return closeColor(b.String(), colored)
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		b.WriteString(s[i : i+size])
		seen++
		i += size
	}
	return s
}

// escapeLen is the length of the color escape beginning at i, or zero if there
// is none there.
//
// Three things walk these strings — measuring one, cutting one, and drawing one
// — and they have to agree about where an escape starts and ends. They each used
// to answer it for themselves.
func escapeLen(s string, i int) int {
	if i+1 >= len(s) || s[i] != '\033' || s[i+1] != '[' {
		return 0
	}
	j := i + 2
	for j < len(s) && s[j] != 'm' {
		j++
	}
	if j < len(s) {
		j++
	}
	return j - i
}

// closeColor ends a color that a cut left open, which would otherwise take the
// rest of the row.
func closeColor(s string, colored bool) string {
	if colored {
		return s + cReset
	}
	return s
}
