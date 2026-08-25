package main

import (
	"bufio"
	"strings"
	"sync"
	"unicode"

	"github.com/leavesafe/leavesafe/internal/safe"
)

// The line the user types on.
//
// Until now there was no such line. The terminal echoed keystrokes wherever the
// cursor happened to be, which was inside the scrolling log — so a sensor
// reporting itself while somebody was half-way through typing "disarm" put its
// message on the same row as the half-typed word, and the word was still there
// afterwards with the log wrapped around it. Nothing was wrong with either the
// message or the command; they were simply written to the same place by two
// things that did not know about each other.
//
// So the keyboard is read a keystroke at a time and the typing is drawn by this
// program, on a row of its own below the log and outside the region that
// scrolls. That row is the only place a typed character ever appears, and it is
// redrawn after anything else is written — which makes "a log line arrived while
// I was typing" a repaint rather than a collision.
//
// The price is that everything the terminal's line discipline used to do has to
// be done here: backspace, the cursor keys, Ctrl+C, Ctrl+Z. That is what the
// rest of this file is.
const (
	// promptText is what marks the row as the one to type on.
	promptText = "> "
	// maskRune stands in for each character of something that must not be read
	// over the user's shoulder. An asterisk rather than a bullet because this
	// row is drawn on Windows consoles too, where a legacy code page turns
	// anything outside ASCII into a different character entirely.
	maskRune = '*'
)

// keyCode names a keystroke that does something other than put a character on
// the line.
type keyCode int

const (
	keyChar keyCode = iota
	keyEnter
	keyBackspace
	keyDelete
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyPrev
	keyNext
	keyKillLine
	keyKillWord
	keyInterrupt
	keyEOF
	keySuspend
	// keyIgnore is a keystroke this line has no use for. It is a code of its
	// own rather than an error because the alternative is typing it: an
	// unrecognized escape sequence put on the line as text is exactly the
	// corruption this file exists to stop.
	keyIgnore
)

// key is one keystroke: a character to type, or something to do.
type key struct {
	code keyCode
	r    rune
}

// readKey turns the next keystroke into one key.
//
// The reader is buffered for a reason beyond speed: it is how a lone Escape is
// told apart from the start of an arrow key, which arrives as an Escape and two
// more bytes in the same read.
func readKey(in *bufio.Reader) (key, error) {
	r, _, err := in.ReadRune()
	if err != nil {
		return key{}, err
	}
	if r == 0x1b {
		return escapeKey(in), nil
	}
	return controlKey(r), nil
}

// controlKey is what a single byte means.
func controlKey(r rune) key {
	switch r {
	case '\r', '\n':
		return key{code: keyEnter}
	// Both, because terminals disagree: Unix sends DEL for the backspace key
	// and Windows consoles send BS.
	case 0x7f, 0x08:
		return key{code: keyBackspace}
	case 0x01:
		return key{code: keyHome}
	case 0x05:
		return key{code: keyEnd}
	case 0x02:
		return key{code: keyLeft}
	case 0x06:
		return key{code: keyRight}
	case 0x15:
		return key{code: keyKillLine}
	case 0x17:
		return key{code: keyKillWord}
	// The three the terminal used to turn into signals for us. In raw mode it
	// no longer does, and a Ctrl+C that did nothing would be a program the user
	// cannot stop with the one key everybody tries first.
	case 0x03:
		return key{code: keyInterrupt}
	case 0x04:
		return key{code: keyEOF}
	case 0x1a:
		return key{code: keySuspend}
	}
	if unicode.IsControl(r) {
		return key{code: keyIgnore}
	}
	return key{code: keyChar, r: r}
}

// escapeKey reads the rest of an escape sequence and says which key sent it.
func escapeKey(in *bufio.Reader) key {
	// Nothing behind it in the same read means the Escape key itself rather
	// than the beginning of something. There is nothing for this line to do
	// with it, and swallowing it is the one answer that cannot go on screen.
	if in.Buffered() == 0 {
		return key{code: keyIgnore}
	}

	intro, _, err := in.ReadRune()
	if err != nil || (intro != '[' && intro != 'O') {
		return key{code: keyIgnore}
	}

	var params []rune
	for {
		r, _, err := in.ReadRune()
		if err != nil {
			return key{code: keyIgnore}
		}
		// The final byte of a sequence is the one in this range; everything
		// before it is parameters, which is where a modifier such as the Ctrl
		// in Ctrl+Left arrives.
		if r >= '@' && r <= '~' {
			return escapeFinal(string(params), r)
		}
		params = append(params, r)
	}
}

// escapeFinal is which key an escape sequence names.
//
// A modified arrow — Ctrl+Left arrives as "1;5D" — is taken as the arrow. The
// modifier means nothing to a single line of text, and dropping the keystroke
// entirely would leave a cursor key that works until you hold Ctrl.
func escapeFinal(params string, final rune) key {
	switch final {
	case 'A':
		return key{code: keyPrev}
	case 'B':
		return key{code: keyNext}
	case 'C':
		return key{code: keyRight}
	case 'D':
		return key{code: keyLeft}
	case 'H':
		return key{code: keyHome}
	case 'F':
		return key{code: keyEnd}
	case '~':
		return tildeKey(params)
	}
	return key{code: keyIgnore}
}

// tildeKey is which key one of the numbered sequences names. Home, End and
// Delete arrive this way from some terminals and as a letter from others.
func tildeKey(params string) key {
	switch params {
	case "1", "7":
		return key{code: keyHome}
	case "3":
		return key{code: keyDelete}
	case "4", "8":
		return key{code: keyEnd}
	}
	return key{code: keyIgnore}
}

// action is what a keystroke asked the program to do, for the keystrokes that
// ask for something beyond changing the line.
type action int

const (
	actNone action = iota
	actSubmit
	actInterrupt
	actEOF
	actSuspend
)

// inputLine is what has been typed so far, where the cursor is in it, and what
// was typed before it.
//
// It holds no writer and takes no lock. Drawing it is the status bar's job,
// under the status bar's lock, because the row it is drawn on is part of the
// same screen as everything else and only one thing may be writing to that at a
// time.
type inputLine struct {
	buf []rune
	pos int

	// history is what has been entered, oldest first, and at is where the
	// cursor keys have walked back to. at == len(history) means the line being
	// typed now rather than one from before.
	history []string
	at      int
	// draft is the line that was being typed when the user started walking
	// back, kept so that walking forward again returns it rather than losing
	// it.
	draft string

	// masked draws the line as asterisks. The PIN is what stands between
	// whoever is holding the laptop and the alarm it is sounding, and the
	// terminal used to echo it in the clear onto a screen the person taking
	// the laptop is looking at.
	masked bool
}

// apply gives one keystroke to the line and says what, if anything, it asked
// for beyond changing it.
func (p *inputLine) apply(k key) action {
	switch k.code {
	case keyChar:
		p.insert(k.r)
	case keyEnter:
		return actSubmit
	case keyBackspace:
		p.deleteBack()
	case keyDelete:
		p.deleteForward()
	case keyLeft:
		p.pos = max(p.pos-1, 0)
	case keyRight:
		p.pos = min(p.pos+1, len(p.buf))
	case keyHome:
		p.pos = 0
	case keyEnd:
		p.pos = len(p.buf)
	case keyPrev:
		p.walk(-1)
	case keyNext:
		p.walk(1)
	case keyKillLine:
		p.set("")
	case keyKillWord:
		p.deleteWord()
	case keyInterrupt:
		return actInterrupt
	case keySuspend:
		return actSuspend
	case keyEOF:
		return p.endOfFile()
	}
	return actNone
}

// endOfFile is what Ctrl+D means, which depends on whether anything has been
// typed: on an empty line it is the end of input, and on a line with something
// on it every terminal in the world deletes the character under the cursor.
func (p *inputLine) endOfFile() action {
	if len(p.buf) == 0 {
		return actEOF
	}
	p.deleteForward()
	return actNone
}

func (p *inputLine) insert(r rune) {
	p.buf = append(p.buf, 0)
	copy(p.buf[p.pos+1:], p.buf[p.pos:])
	p.buf[p.pos] = r
	p.pos++
}

func (p *inputLine) deleteBack() {
	if p.pos == 0 {
		return
	}
	p.buf = append(p.buf[:p.pos-1], p.buf[p.pos:]...)
	p.pos--
}

func (p *inputLine) deleteForward() {
	if p.pos >= len(p.buf) {
		return
	}
	p.buf = append(p.buf[:p.pos], p.buf[p.pos+1:]...)
}

// deleteWord removes the word before the cursor, along with any space between
// the two. The space in front of that word is left where it is, which is what
// every shell does: "trigger power" becomes "trigger " and is one word away
// from being a different command rather than a rewritten one.
func (p *inputLine) deleteWord() {
	at := p.pos
	for at > 0 && p.buf[at-1] == ' ' {
		at--
	}
	for at > 0 && p.buf[at-1] != ' ' {
		at--
	}
	p.buf = append(p.buf[:at], p.buf[p.pos:]...)
	p.pos = at
}

// set replaces the whole line and puts the cursor at the end of it.
func (p *inputLine) set(s string) {
	p.buf = []rune(s)
	p.pos = len(p.buf)
}

// walk steps through what has been typed before.
func (p *inputLine) walk(by int) {
	to := p.at + by
	if to < 0 || to > len(p.history) {
		return
	}
	if p.at == len(p.history) {
		p.draft = string(p.buf)
	}

	p.at = to
	if to == len(p.history) {
		p.set(p.draft)
		return
	}
	p.set(p.history[to])
}

// take hands back what was typed and leaves an empty line behind.
func (p *inputLine) take() string {
	line := string(p.buf)
	p.buf, p.pos, p.draft = nil, 0, ""

	// A masked line is never remembered: a PIN in the history is a PIN one
	// press of the up arrow away from being on screen.
	if trimmed := strings.TrimSpace(line); trimmed != "" && !p.masked && p.lastTyped() != trimmed {
		p.history = append(p.history, trimmed)
	}
	p.at = len(p.history)
	return line
}

// lastTyped is the most recent command remembered, or "" if there is none.
// Repeating it is not worth a second entry — the history is walked with one
// key, and a run of the same command makes that key useless.
func (p *inputLine) lastTyped() string {
	if len(p.history) == 0 {
		return ""
	}
	return p.history[len(p.history)-1]
}

// view is the part of the line that fits in a window this wide, and the column
// the cursor goes in.
//
// The line can be longer than the window — an address pasted in, a command with
// a long argument — and what has to stay visible is wherever the cursor is,
// because that is where the next character will land.
func (p *inputLine) view(width int) (text string, cursorCol int) {
	// One short of the width: a character written in the last cell of a row
	// leaves some terminals with a wrap pending, and this row is the one row
	// that must not scroll.
	room := max(width-len(promptText)-1, 1)

	from := 0
	if p.pos >= room {
		from = p.pos - room + 1
	}
	shown := p.buf[from:min(from+room, len(p.buf))]
	if p.masked {
		shown = []rune(strings.Repeat(string(maskRune), len(shown)))
	}
	// Columns are 1-based, which is what the escape that moves the cursor
	// expects.
	return promptText + string(shown), len(promptText) + (p.pos - from) + 1
}

// lineReader is where a typed line comes from.
//
// Two things implement it. On a dashboard the program owns the screen and reads
// the keyboard a keystroke at a time; anywhere else — piped input, a -plain run,
// a test — the terminal is not ours to take over and lines are read as lines.
type lineReader interface {
	// readLine returns the next line typed, and false when there will be no
	// more.
	readLine() (string, bool)
	// readSecret is readLine for something that must not be left on screen.
	readSecret() (string, bool)
}

// scannedLines reads whole lines from something that is not a terminal this
// program has taken over.
type scannedLines struct{ sc *bufio.Scanner }

func (s scannedLines) readLine() (string, bool) {
	if !s.sc.Scan() {
		return "", false
	}
	return s.sc.Text(), true
}

// readSecret is the same read. Without the terminal in raw mode the echo
// belongs to the terminal, and there is nothing here that could stop it.
func (s scannedLines) readSecret() (string, bool) { return s.readLine() }

// typedLines reads the keyboard a keystroke at a time and draws what is typed
// on the dashboard's own input row.
//
// Reading the keyboard and acting on it are two goroutines, and that split is
// the whole of this type. It used to be one: the loop read a keystroke, and
// when the keystroke finished a line it ran the command itself before reading
// the next one. So for as long as a command took, nothing was reading the
// keyboard — and in raw mode nothing else turns Ctrl+C into anything. Arming
// while a sensor was still being asked whether it works could take the better
// part of a minute, and for that whole minute the terminal was a screen with
// no echo, no commands, no Ctrl+C and no Ctrl+Z: the user could not get out of
// it without killing the process from somewhere else, which left the terminal
// in raw mode afterwards.
//
// So [typedLines.pump] does nothing but read. It answers the two keystrokes
// that mean "stop" and "go away" on the spot, whatever else the program is
// busy with, and hands everything else to whoever is asking for a line.
//
// What it deliberately does *not* do is put those keystrokes on screen. A
// keystroke is drawn by the reader that consumes it, which is the one that
// knows whether this line is a command or a PIN — read ahead and drawn by the
// pump, a PIN typed before the prompt appeared would go on screen in the clear.
type typedLines struct {
	in *bufio.Reader
	sb *statusBar
	// quit ends the program, for the Ctrl+C the terminal no longer turns into
	// a signal on our behalf.
	quit func()

	// keys is what the pump has read and nobody has acted on yet, and started
	// is what makes sure there is exactly one pump behind it.
	started sync.Once
	keys    chan key
}

// keyBacklog is how many keystrokes may be waiting to be acted on.
//
// It exists so that the pump never blocks: a pump waiting for somebody to take
// a keystroke is a pump not reading the next one, which is the deafness this
// whole arrangement exists to remove. Far more than anybody types ahead, and a
// keystroke past it is dropped the way a full terminal buffer drops one.
const keyBacklog = 256

// begin starts the one goroutine that reads the keyboard.
func (t *typedLines) begin() {
	t.started.Do(func() {
		t.keys = make(chan key, keyBacklog)
		safe.Go("console-keys", t.pump)
	})
}

// pump reads the keyboard for as long as there is one, and does nothing else.
//
// Ctrl+C and Ctrl+Z are answered here rather than passed on, because they are
// the two keystrokes whose whole value is that they work when nothing else
// does. Neither needs to know what is on the line, so neither has to wait for
// somebody to be asking for one.
//
// Everything else is handed on. It is not applied to the line and not drawn:
// see the note on the type.
func (t *typedLines) pump() {
	defer close(t.keys)

	for {
		k, err := readKey(t.in)
		if err != nil {
			return
		}

		switch k.code {
		case keyInterrupt:
			t.quit()
			return
		case keySuspend:
			suspendFn()
			continue
		}

		select {
		case t.keys <- k:
		default:
			// Dropped rather than waited on. See keyBacklog.
		}
	}
}

func (t *typedLines) readLine() (string, bool) {
	line, ok := t.read()
	if ok && strings.TrimSpace(line) != "" {
		// Echoed into the log, because the row it was typed on is about to be
		// cleared for the next command and a reply with no question above it
		// reads as something the program decided on its own.
		t.sb.writeLine("  %s%s%s", cDim, promptText+line, cReset)
	}
	return line, ok
}

func (t *typedLines) readSecret() (string, bool) {
	t.sb.maskLine(true)
	defer t.sb.maskLine(false)
	return t.read()
}

// read applies keystrokes to the line until one of them finishes it.
//
// This is where a keystroke reaches the screen, and it runs only while
// somebody is asking for a line — which is what makes the masking on the line
// below trustworthy. Keystrokes typed while a command was running are waiting
// in the channel by the time this is called, so they are drawn now, under
// whatever this caller asked for, rather than when they were typed.
func (t *typedLines) read() (string, bool) {
	t.begin()

	for {
		k, ok := <-t.keys
		if !ok {
			return "", false
		}

		switch t.sb.applyKey(k) {
		case actSubmit:
			return t.sb.takeLine(), true
		case actInterrupt, actEOF:
			t.quit()
			return "", false
		case actSuspend:
			// The pump answers Ctrl+Z, so this is only reachable from a
			// keystroke that arrived before it did. Answering it twice is
			// cheaper than an arm of this switch that silently does nothing.
			suspendFn()
		case actNone:
		}
	}
}

// suspendFn is requestSuspend, as a variable so a test can stand in for it: a
// test that let the real one run would stop the test binary, and nothing in the
// run would ever continue it. Nothing in the running program reassigns it.
var suspendFn = requestSuspend

// newLineReader picks how typed lines are read for this run.
func newLineReader(in *bufio.Reader, sb *statusBar, quit func()) lineReader {
	if sb != nil && sb.readsKeystrokes() {
		return &typedLines{in: in, sb: sb, quit: quit}
	}
	return scannedLines{sc: bufio.NewScanner(in)}
}
