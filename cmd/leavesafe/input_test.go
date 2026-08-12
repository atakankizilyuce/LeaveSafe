package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/remote"
)

// The input line is what a person types on, and until it existed the terminal
// echoed their keystrokes into the middle of the scrolling log. These tests are
// about the two halves of getting that right: reading the keyboard a keystroke
// at a time, which means being the line discipline rather than relying on one,
// and drawing what was typed somewhere the log cannot reach.
//
// A keystroke that is misread is not a cosmetic fault here. An arrow key put on
// the line as "[A" is a command the console will refuse; a Ctrl+C that is not
// recognized is a program the user cannot stop with the key everybody tries
// first.

// keyed reads one key from a string, the way the console reads one from a
// terminal.
func keyed(t *testing.T, typed string) key {
	t.Helper()

	k, err := readKey(bufio.NewReader(strings.NewReader(typed)))
	if err != nil {
		t.Fatalf("readKey(%q): %v", typed, err)
	}
	return k
}

// dripReader hands over one byte per read, which is how a terminal delivers a
// key pressed on its own: the Escape arrives, and there is nothing behind it.
// Read from a string all at once, every Escape looks like the start of an arrow.
type dripReader struct{ left string }

func (d *dripReader) Read(p []byte) (int, error) {
	if d.left == "" {
		return 0, io.EOF
	}
	p[0] = d.left[0]
	d.left = d.left[1:]
	return 1, nil
}

func TestATypedCharacterGoesOntoTheLineAsItself(t *testing.T) {
	for _, typed := range []string{"a", "7", "ş"} {
		k := keyed(t, typed)
		if k.code != keyChar || string(k.r) != typed {
			t.Errorf("%q read as {code %v, rune %q}, want it typed as itself", typed, k.code, k.r)
		}
	}
}

// Everything the terminal's line discipline used to do for us, which it stops
// doing the moment the keyboard is read raw.
func TestTheKeysThatEditTheLineAreRecognized(t *testing.T) {
	cases := map[string]struct {
		typed string
		want  keyCode
	}{
		"enter":                   {"\r", keyEnter},
		"enter as a newline":      {"\n", keyEnter},
		"backspace on unix":       {"\x7f", keyBackspace},
		"backspace on a console":  {"\b", keyBackspace},
		"start of the line":       {"\x01", keyHome},
		"end of the line":         {"\x05", keyEnd},
		"back one character":      {"\x02", keyLeft},
		"on one character":        {"\x06", keyRight},
		"throw the line away":     {"\x15", keyKillLine},
		"throw the last word out": {"\x17", keyKillWord},
		"quit":                    {"\x03", keyInterrupt},
		"end of input":            {"\x04", keyEOF},
		"suspend":                 {"\x1a", keySuspend},
		// A control character with no meaning here is swallowed rather than
		// typed: putting it on the line would put an unprintable byte into a
		// command the console then reports as unknown.
		"something with no meaning here": {"\v", keyIgnore},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := keyed(t, tc.typed).code; got != tc.want {
				t.Errorf("%q read as %v, want %v", tc.typed, got, tc.want)
			}
		})
	}
}

// The cursor keys arrive as escape sequences, and terminals disagree about
// which. All of them have to be read as the key that was pressed — the one
// thing that must never happen is the sequence itself appearing on the line.
func TestTheCursorKeysAreReadAsKeysRatherThanTypedAsText(t *testing.T) {
	cases := map[string]struct {
		typed string
		want  keyCode
	}{
		"up":               {"\033[A", keyPrev},
		"down":             {"\033[B", keyNext},
		"right":            {"\033[C", keyRight},
		"left":             {"\033[D", keyLeft},
		"home as a letter": {"\033[H", keyHome},
		"end as a letter":  {"\033[F", keyEnd},
		"the other left":   {"\033OD", keyLeft},
		"home as a number": {"\033[1~", keyHome},
		"the other home":   {"\033[7~", keyHome},
		"delete":           {"\033[3~", keyDelete},
		"end as a number":  {"\033[4~", keyEnd},
		"the other end":    {"\033[8~", keyEnd},
		"page up, which is not a key this line has": {"\033[5~", keyIgnore},
		// Ctrl+Left. The modifier means nothing to a single line of text, so it
		// is the arrow that counts — a cursor key that stops working the moment
		// you hold Ctrl is worse than one that ignores the modifier.
		"left with a modifier": {"\033[1;5D", keyLeft},
		// A sequence this line has never heard of. Swallowed, because the
		// alternative is typing it.
		"something else entirely": {"\033[?1000h", keyIgnore},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := keyed(t, tc.typed).code; got != tc.want {
				t.Errorf("%q read as %v, want %v", tc.typed, got, tc.want)
			}
		})
	}
}

// Escape pressed on its own arrives with nothing behind it, which is what tells
// it apart from the start of an arrow key.
func TestEscapePressedOnItsOwnIsSwallowed(t *testing.T) {
	k, err := readKey(bufio.NewReader(&dripReader{left: "\033"}))
	if err != nil {
		t.Fatalf("readKey: %v", err)
	}
	if k.code != keyIgnore {
		t.Errorf("a lone Escape read as %v, want it swallowed", k.code)
	}
}

// A sequence cut off half-way — a connection that dropped mid-keystroke — is
// swallowed rather than typed as whatever arrived before the break.
func TestAnEscapeSequenceThatStopsHalfWayIsSwallowed(t *testing.T) {
	for _, typed := range []string{"\033[", "\033[1;", "\033x"} {
		if got := keyed(t, typed).code; got != keyIgnore {
			t.Errorf("%q read as %v, want it swallowed", typed, got)
		}
	}
}

func TestReadKeyReportsInputThatHasStopped(t *testing.T) {
	if _, err := readKey(bufio.NewReader(strings.NewReader(""))); err == nil {
		t.Error("a reader with nothing left did not report the end of input")
	}
}

// ---- editing the line ----------------------------------------------------

// typeInto plays a string of keys into a line, one at a time.
func typeInto(p *inputLine, keys ...key) {
	for _, k := range keys {
		p.apply(k)
	}
}

// chars is what a typed word arrives as.
func chars(s string) []key {
	keys := make([]key, 0, len(s))
	for _, r := range s {
		keys = append(keys, key{code: keyChar, r: r})
	}
	return keys
}

func TestBackspaceRemovesTheCharacterBeforeTheCursor(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("arm")...)
	typeInto(p, key{code: keyBackspace})

	if got := string(p.buf); got != "ar" {
		t.Errorf("line is %q after a backspace, want %q", got, "ar")
	}
	// Nothing to delete is not an error, and must not walk off the front.
	typeInto(p, key{code: keyHome}, key{code: keyBackspace})
	if got := string(p.buf); got != "ar" {
		t.Errorf("backspace at the start of the line changed it to %q", got)
	}
}

// A character typed in the middle of a line goes in the middle of it. Appended
// instead, correcting a typo would mean deleting everything after it.
func TestACharacterTypedInTheMiddleGoesInTheMiddle(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("am")...)
	typeInto(p, key{code: keyLeft})
	typeInto(p, chars("r")...)

	if got := string(p.buf); got != "arm" {
		t.Errorf("line is %q, want %q", got, "arm")
	}
	if p.pos != 2 {
		t.Errorf("cursor at %d, want it after what was just typed", p.pos)
	}
}

func TestDeleteRemovesTheCharacterUnderTheCursor(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("arm")...)
	typeInto(p, key{code: keyHome}, key{code: keyDelete})

	if got := string(p.buf); got != "rm" {
		t.Errorf("line is %q after a delete, want %q", got, "rm")
	}
	// At the end there is nothing under the cursor to remove.
	typeInto(p, key{code: keyEnd}, key{code: keyDelete})
	if got := string(p.buf); got != "rm" {
		t.Errorf("delete at the end of the line changed it to %q", got)
	}
}

func TestTheCursorStopsAtBothEndsOfTheLine(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("qr 2")...)

	typeInto(p, key{code: keyRight})
	if p.pos != len(p.buf) {
		t.Errorf("cursor at %d after right at the end, want %d", p.pos, len(p.buf))
	}
	typeInto(p, key{code: keyHome}, key{code: keyLeft})
	if p.pos != 0 {
		t.Errorf("cursor at %d after left at the start, want 0", p.pos)
	}
	typeInto(p, key{code: keyEnd})
	if p.pos != len(p.buf) {
		t.Errorf("end key left the cursor at %d, want %d", p.pos, len(p.buf))
	}
}

func TestThrowingTheLineAwayLeavesAnEmptyOne(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("trigger power")...)
	typeInto(p, key{code: keyKillLine})

	if len(p.buf) != 0 || p.pos != 0 {
		t.Errorf("line is %q with the cursor at %d, want it empty", string(p.buf), p.pos)
	}
}

// The argument goes and the command stays, which is one keystroke away from
// naming a different sensor rather than typing the whole line again.
func TestThrowingTheLastWordOutLeavesTheCommandStanding(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("trigger power")...)
	typeInto(p, key{code: keyKillWord})

	if got := string(p.buf); got != "trigger " {
		t.Errorf("line is %q, want %q", got, "trigger ")
	}
	typeInto(p, key{code: keyKillWord})
	if got := string(p.buf); got != "" {
		t.Errorf("line is %q after the second word went, want it empty", got)
	}
}

// Ctrl+D means two different things, and which one depends on whether there is
// anything on the line — as it does in every shell.
func TestCtrlDEndsAnEmptyLineAndEditsOneWithSomethingOnIt(t *testing.T) {
	empty := &inputLine{}
	if got := empty.apply(key{code: keyEOF}); got != actEOF {
		t.Errorf("Ctrl+D on an empty line asked for %v, want the end of input", got)
	}

	p := &inputLine{}
	typeInto(p, chars("arm")...)
	typeInto(p, key{code: keyHome})
	if got := p.apply(key{code: keyEOF}); got != actNone {
		t.Errorf("Ctrl+D on a line with something on it asked for %v, want nothing", got)
	}
	if got := string(p.buf); got != "rm" {
		t.Errorf("line is %q, want %q", got, "rm")
	}
}

func TestTheKeysThatAskForSomethingSayWhatTheyAskedFor(t *testing.T) {
	cases := map[keyCode]action{
		keyEnter:     actSubmit,
		keyInterrupt: actInterrupt,
		keySuspend:   actSuspend,
	}
	for code, want := range cases {
		if got := (&inputLine{}).apply(key{code: code}); got != want {
			t.Errorf("key %v asked for %v, want %v", code, got, want)
		}
	}
}

// ---- what was typed before -----------------------------------------------

func TestTheLastCommandComesBackWithTheUpArrow(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("arm")...)
	p.take()
	typeInto(p, chars("status")...)
	p.take()

	typeInto(p, key{code: keyPrev})
	if got := string(p.buf); got != "status" {
		t.Errorf("the first step back gave %q, want %q", got, "status")
	}
	typeInto(p, key{code: keyPrev})
	if got := string(p.buf); got != "arm" {
		t.Errorf("the second step back gave %q, want %q", got, "arm")
	}
	// There is nothing older, and walking past the end must not throw away
	// what is on the line.
	typeInto(p, key{code: keyPrev})
	if got := string(p.buf); got != "arm" {
		t.Errorf("walking past the oldest command gave %q, want it left alone", got)
	}
}

// Walking back through the history and forward again has to return the line
// that was being typed. Losing it means the up arrow is a key that silently
// throws away half-written commands.
func TestWalkingBackAndForwardReturnsTheLineThatWasBeingTyped(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("arm")...)
	p.take()

	typeInto(p, chars("dis")...)
	typeInto(p, key{code: keyPrev})
	if got := string(p.buf); got != "arm" {
		t.Fatalf("the step back gave %q, want %q", got, "arm")
	}

	typeInto(p, key{code: keyNext})
	if got := string(p.buf); got != "dis" {
		t.Errorf("coming back gave %q, want the half-typed %q", got, "dis")
	}
	// And there is nothing newer than the line being typed.
	typeInto(p, key{code: keyNext})
	if got := string(p.buf); got != "dis" {
		t.Errorf("walking past the newest gave %q, want it left alone", got)
	}
}

func TestTheHistoryKeepsNeitherBlanksNorRepeats(t *testing.T) {
	p := &inputLine{}
	for _, typed := range []string{"arm", "arm", "   ", "status"} {
		typeInto(p, chars(typed)...)
		p.take()
	}

	if len(p.history) != 2 {
		t.Fatalf("history is %q, want one entry each for arm and status", p.history)
	}
	if p.history[0] != "arm" || p.history[1] != "status" {
		t.Errorf("history is %q, want [arm status]", p.history)
	}
}

func TestTakingTheLineLeavesAnEmptyOneBehind(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("history")...)

	if got := p.take(); got != "history" {
		t.Fatalf("take gave %q, want %q", got, "history")
	}
	if len(p.buf) != 0 || p.pos != 0 {
		t.Errorf("line is %q with the cursor at %d, want it empty", string(p.buf), p.pos)
	}
}

// A PIN in the history is a PIN one press of the up arrow away from being on
// screen, on a laptop somebody has just picked up.
func TestAMaskedLineIsNeverRemembered(t *testing.T) {
	p := &inputLine{masked: true}
	typeInto(p, chars("2468")...)

	if got := p.take(); got != "2468" {
		t.Fatalf("take gave %q, want the PIN itself", got)
	}
	if len(p.history) != 0 {
		t.Errorf("history is %q, want the PIN forgotten", p.history)
	}
}

// ---- what the row looks like ---------------------------------------------

func TestTheLineIsDrawnAfterThePromptWithTheCursorInIt(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("arm")...)

	text, col := p.view(40)
	if text != promptText+"arm" {
		t.Errorf("row reads %q, want %q", text, promptText+"arm")
	}
	// Columns are 1-based: the prompt, the three characters, and the cursor
	// after them.
	if want := len(promptText) + 4; col != want {
		t.Errorf("cursor at column %d, want %d", col, want)
	}
}

// A line longer than the window scrolls sideways, and what has to stay on
// screen is wherever the cursor is: that is where the next character lands.
func TestALineTooLongForTheWindowKeepsTheCursorInView(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("trigger power and then some more")...)

	text, col := p.view(16)
	if visLen(text) > 16 {
		t.Errorf("row is %d columns wide in a 16-column window: %q", visLen(text), text)
	}
	if !strings.HasSuffix(text, "more") {
		t.Errorf("row reads %q, want it to end where the cursor is", text)
	}
	if col < 1 || col > 16 {
		t.Errorf("cursor at column %d, which is not in the window", col)
	}

	// Walk back to the start and the other end of the line comes into view.
	typeInto(p, key{code: keyHome})
	text, col = p.view(16)
	if !strings.HasPrefix(text, promptText+"trigger") {
		t.Errorf("row reads %q, want the start of the line", text)
	}
	if col != len(promptText)+1 {
		t.Errorf("cursor at column %d, want it on the first character", col)
	}
}

// A window with no room at all still has to produce a row rather than a panic.
func TestAWindowWithNoRoomStillDrawsSomething(t *testing.T) {
	p := &inputLine{}
	typeInto(p, chars("arm")...)

	text, col := p.view(1)
	if text == "" || col < 1 {
		t.Errorf("row is %q with the cursor at %d, want something drawable", text, col)
	}
}

func TestAMaskedLineShowsNothingOfWhatWasTyped(t *testing.T) {
	p := &inputLine{masked: true}
	typeInto(p, chars("2468")...)

	text, _ := p.view(40)
	if strings.Contains(text, "2468") {
		t.Errorf("row reads %q, which is the PIN itself", text)
	}
	if want := promptText + "****"; text != want {
		t.Errorf("row reads %q, want %q", text, want)
	}
}

// ---- reading lines -------------------------------------------------------

func TestLinesAreReadWholeWhenTheTerminalIsNotOurs(t *testing.T) {
	r := newLineReader(bufio.NewReader(strings.NewReader("arm\ndisarm\n")), nil, nil)

	for _, want := range []string{"arm", "disarm"} {
		got, ok := r.readLine()
		if !ok || got != want {
			t.Fatalf("read %q (ok %v), want %q", got, ok, want)
		}
	}
	if _, ok := r.readLine(); ok {
		t.Error("a reader with nothing left reported another line")
	}
}

// Without the keyboard in raw mode the echo belongs to the terminal, and there
// is nothing here that could stop it. Saying so plainly is better than a method
// that pretends to hide something.
func TestASecretReadFromAPipeIsJustALine(t *testing.T) {
	r := newLineReader(bufio.NewReader(strings.NewReader("2468\n")), nil, nil)

	got, ok := r.readSecret()
	if !ok || got != "2468" {
		t.Errorf("read %q (ok %v), want the line as typed", got, ok)
	}
}

// typingFixture is a dashboard with an input line, driven by the keystrokes a
// person would type at it.
func typingFixture(t *testing.T, typed string) (*typedLines, *statusBar, *syncBuffer) {
	t.Helper()

	screen := &syncBuffer{}
	sb := &statusBar{
		out: screen, hub: testHub(t), sensorMgr: monitor.NewManager(),
		key: testKey, rawKey: testRawKey, line: &inputLine{},
	}
	sb.paint()

	reader := newLineReader(bufio.NewReader(strings.NewReader(typed)), sb, func() {})
	typing, ok := reader.(*typedLines)
	if !ok {
		t.Fatalf("a dashboard reading the keyboard produced a %T, want the keystroke reader", reader)
	}
	return typing, sb, screen
}

func TestATypedCommandIsReadWhenEnterIsPressed(t *testing.T) {
	typing, _, screen := typingFixture(t, "arm\r")

	got, ok := typing.readLine()
	if !ok || got != "arm" {
		t.Fatalf("read %q (ok %v), want %q", got, ok, "arm")
	}
	// Echoed into the log, because the row it was typed on is cleared for the
	// next command: a reply with no question above it reads as something the
	// program decided on its own.
	if !strings.Contains(screen.String(), promptText+"arm") {
		t.Errorf("the command was not echoed into the log; screen was:\n%q", screen.String())
	}
}

// Backspace has to work through the reader as well as on the line: this is the
// path a person actually takes when they mistype.
func TestACorrectedCommandIsReadAsWhatWasLeft(t *testing.T) {
	typing, _, _ := typingFixture(t, "arn\x7fm\r")

	got, ok := typing.readLine()
	if !ok || got != "arm" {
		t.Errorf("read %q (ok %v), want %q", got, ok, "arm")
	}
}

// Nothing typed is nothing to echo. A bare enter putting an empty prompt into
// the log would fill the screen with them.
func TestAnEmptyLineIsNotEchoedIntoTheLog(t *testing.T) {
	typing, _, screen := typingFixture(t, "\r")
	before := screen.String()

	if _, ok := typing.readLine(); !ok {
		t.Fatal("a bare enter did not produce a line")
	}
	if added := strings.TrimPrefix(screen.String(), before); strings.Contains(added, promptText+"\n") {
		t.Errorf("an empty line was echoed into the log; what was added was:\n%q", added)
	}
}

// In raw mode the terminal no longer turns Ctrl+C into a signal, so a program
// that did not answer it itself would be one the user cannot stop with the key
// everybody tries first.
func TestCtrlCEndsTheProgram(t *testing.T) {
	screen := &syncBuffer{}
	sb := &statusBar{out: screen, hub: testHub(t), sensorMgr: monitor.NewManager(), line: &inputLine{}}
	sb.paint()

	quit := 0
	typing := &typedLines{in: bufio.NewReader(strings.NewReader("\x03")), sb: sb, quit: func() { quit++ }}

	if _, ok := typing.readLine(); ok {
		t.Error("Ctrl+C produced a command instead of ending the program")
	}
	if quit != 1 {
		t.Errorf("the program was asked to quit %d times, want once", quit)
	}
}

// Ctrl+D on an empty line is the other way out, and it has to be the same one:
// a terminal that closed and a user who pressed it mean the same thing.
func TestCtrlDOnAnEmptyLineEndsTheProgram(t *testing.T) {
	screen := &syncBuffer{}
	sb := &statusBar{out: screen, hub: testHub(t), sensorMgr: monitor.NewManager(), line: &inputLine{}}
	sb.paint()

	quit := 0
	typing := &typedLines{in: bufio.NewReader(strings.NewReader("\x04")), sb: sb, quit: func() { quit++ }}

	if _, ok := typing.readLine(); ok {
		t.Error("Ctrl+D produced a command instead of ending the program")
	}
	if quit != 1 {
		t.Errorf("the program was asked to quit %d times, want once", quit)
	}
}

func TestInputThatStopsEndsTheLoopRatherThanTheProgram(t *testing.T) {
	typing, _, _ := typingFixture(t, "")

	if _, ok := typing.readLine(); ok {
		t.Error("a reader with nothing in it produced a line")
	}
}

// Ctrl+Z is a keystroke now rather than a signal, so it has to be turned back
// into one. The real call would stop the test binary, and nothing in the run
// would ever continue it.
func TestCtrlZAsksForTheProgramToBeSuspended(t *testing.T) {
	asked := 0
	previous := suspendFn
	suspendFn = func() { asked++ }
	t.Cleanup(func() { suspendFn = previous })

	typing, _, _ := typingFixture(t, "\x1a")

	if _, ok := typing.readLine(); ok {
		t.Error("Ctrl+Z produced a command")
	}
	if asked != 1 {
		t.Errorf("a suspend was asked for %d times, want once", asked)
	}
}

// The PIN is what stands between whoever is holding the laptop and the alarm it
// is sounding. Echoed in the clear it is shown to exactly the person it is
// meant to stop, on a screen they are already looking at.
func TestAPinIsNeitherShownNorEchoed(t *testing.T) {
	typing, sb, screen := typingFixture(t, "2468\r")

	got, ok := typing.readSecret()
	if !ok || got != "2468" {
		t.Fatalf("read %q (ok %v), want the PIN", got, ok)
	}
	if strings.Contains(screen.String(), "2468") {
		t.Errorf("the PIN was drawn on screen; screen was:\n%q", screen.String())
	}
	if !strings.Contains(screen.String(), "****") {
		t.Errorf("nothing was drawn while the PIN was typed; screen was:\n%q", screen.String())
	}
	// And the masking is put back afterwards, or every command from here on
	// would be typed into asterisks.
	if sb.line.masked {
		t.Error("the line is still masked after the PIN was read")
	}
}

// ---- where the typing is drawn -------------------------------------------

// The whole point of the row: it is below the region that scrolls, so nothing
// the log does can reach it. Inside the region, a busy minute would scroll what
// the user is typing off the top of it a line at a time.
func TestTheInputRowIsBelowEverythingThatScrolls(t *testing.T) {
	_, sb, screen := typingFixture(t, "")

	// The window a status bar with nowhere to ask lays out for.
	const windowRows = 40
	if sb.layout.termH != windowRows-1 {
		t.Fatalf("the layout was given %d of %d rows, want one held back for the input line",
			sb.layout.termH, windowRows)
	}
	if want := fmt.Sprintf("\033[%d;%dr", sb.layout.logRow, windowRows-1); !strings.Contains(screen.String(), want) {
		t.Errorf("the scrolling region does not stop above the input row; wanted %q in:\n%q", want, screen.String())
	}
	if want := fmt.Sprintf("\033[%d;1H\033[2K%s", windowRows, promptText); !strings.Contains(screen.String(), want) {
		t.Errorf("the input row was not drawn at the foot of the window; wanted %q in:\n%q", want, screen.String())
	}
}

// This is the bug the whole file is for. A sensor reporting itself while
// somebody is half-way through typing "disarm" used to write its message onto
// the row they were typing on, because both went to wherever the cursor was.
func TestALogLineWrittenWhileTypingLeavesTheTypingAlone(t *testing.T) {
	_, sb, screen := typingFixture(t, "")
	for _, k := range chars("disarm") {
		sb.applyKey(k)
	}

	before := screen.String()
	sb.writeLine("  [SENSOR] the lid was closed")
	added := strings.TrimPrefix(screen.String(), before)

	// The log is written where the log left off, which is kept in the
	// terminal's cursor store rather than in the cursor: the cursor is on the
	// input row.
	if !strings.HasPrefix(added, cursorRestore) {
		t.Errorf("the log line was written at the cursor, which is where the typing is:\n%q", added)
	}
	if !strings.Contains(added, "the lid was closed\n"+cursorSave) {
		t.Errorf("the log's position was not put back for the next line:\n%q", added)
	}
	// And what was typed is still there afterwards, drawn again on its own row.
	if !strings.HasSuffix(added, redrawnAt(sb, promptText+"disarm")) {
		t.Errorf("the typing was not put back after the log line:\n%q", added)
	}
}

// redrawnAt is the input row as it is drawn: cleared, written, and the cursor
// put at the end of what is on it.
func redrawnAt(sb *statusBar, text string) string {
	row := sb.layout.termH + 1
	return fmt.Sprintf("\033[%d;1H\033[2K%s\033[%d;%dH", row, text, row, len(text)+1)
}

// Without a line of our own the terminal is echoing what is typed, and the log
// is written where it always was. Reaching for the cursor store there would be
// restoring a position nothing ever saved.
func TestWithoutAnInputLineTheLogIsWrittenWhereTheCursorIs(t *testing.T) {
	screen := &syncBuffer{}
	sb := &statusBar{out: screen, hub: testHub(t), sensorMgr: monitor.NewManager()}
	sb.paint()

	before := screen.String()
	sb.writeLine("  [SENSOR] the lid was closed")
	added := strings.TrimPrefix(screen.String(), before)

	if strings.Contains(added, cursorRestore) && !strings.Contains(added, "\033[s") {
		t.Errorf("the log reached for a position nothing saved:\n%q", added)
	}
	if !strings.HasPrefix(added, "  [SENSOR] the lid was closed\n") {
		t.Errorf("the log line was not written at the cursor:\n%q", added)
	}
}

// The input line exists only where there is a keyboard to read: a terminal this
// program could take. That has to be settled before the first draw, because a
// row of the window is held back for it.
func TestADashboardOnATerminalOfItsOwnDrawsAnInputLine(t *testing.T) {
	// The screen is a package-level singleton and the keyboard is part of it,
	// so it is put back for whatever runs next.
	t.Cleanup(func() { terminalScreen.keys = nil })
	aTerminal(t)
	aKeyboard(t, nil, nil)

	sb, drawn := drawnDashboard(t, remote.State{})

	if sb.line == nil {
		t.Fatal("a dashboard with a keyboard of its own has nowhere for the user to type")
	}
	if !strings.Contains(drawn, promptText) {
		t.Errorf("no input row was drawn; screen was:\n%q", drawn)
	}
}

// The editing keys are this program's own now, and nothing about a row with a
// prompt on it suggests that the last command is one press away.
func TestHelpSaysWhereToTypeWhenTheProgramIsReadingTheKeyboard(t *testing.T) {
	_, sb, screen := typingFixture(t, "")

	consoleHelp(context.Background(), &console{consoleDeps: consoleDeps{sb: sb}})

	if !strings.Contains(screen.String(), "walk through what you typed before") {
		t.Errorf("help did not say what the cursor keys do; screen was:\n%q", screen.String())
	}
}
