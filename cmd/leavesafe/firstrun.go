package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/config"
)

// The first screen anybody sees: which language to be asked questions in.
//
// It is drawn rather than printed. The dashboard that follows is a set of boxed
// panels, and a run that opens with loose lines and then snaps into a framed
// layout reads as two programs. The same frame, the same ◉ heading and the same
// two-column option shape make it one.
//
// The language reaches these questions and stops there — see Config.Language.
// The choice is deliberately narrow: Turkish or English, no third option and no
// guess from the operating system's locale. Guessing wrong on the first screen
// costs the user the only question they have to answer before the program runs.

// promptWidth is the outside width of the boxes, chosen to sit inside an
// 80-column terminal with room to spare and to hold the longest line of either
// language without wrapping.
const promptWidth = 64

// language holds the wording for one language. There are two of these, and the
// wording is small enough and stable enough that a table beats a translation
// framework — the alternative is a dependency and a file format for two strings.
type language struct {
	code string

	langTitle  string
	langChoice string
}

var turkish = language{
	code: "tr",

	langTitle:  "DİL · LANGUAGE",
	langChoice: "Seçiminiz / Your choice",
}

var english = language{
	code: "en",

	langTitle:  "LANGUAGE · DİL",
	langChoice: "Your choice / Seçiminiz",
}

// languageByCode returns the wording for a stored code, falling back to English
// for anything unrecognized. Validate already clears those, so this is the
// second lock rather than the first.
func languageByCode(code string) language {
	if code == "tr" {
		return turkish
	}
	return english
}

// ── Drawing ──────────────────────────────────────────────────────────────
//
// Every line goes through boxLine so the right-hand border lands in the same
// column on every row. visLen counts runes rather than bytes, which is the
// whole reason the Turkish text can share a frame with the English: "ğ" is two
// bytes and one column, and measuring in bytes would tear the box apart on
// exactly the language this option exists for.

func boxTop() string {
	return "  ┌" + strings.Repeat("─", promptWidth-2) + "┐"
}

func boxSep() string {
	return "  ├" + strings.Repeat("─", promptWidth-2) + "┤"
}

func boxBottom() string {
	return "  └" + strings.Repeat("─", promptWidth-2) + "┘"
}

// promptLine pads content out to the inner width and closes the border.
func promptLine(content string) string {
	pad := promptWidth - 2 - visLen(content)
	if pad < 0 {
		pad = 0
	}
	return "  │" + content + strings.Repeat(" ", pad) + "│"
}

// promptHeading draws the framed title every question opens with.
func promptHeading(out io.Writer, title string) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, boxTop())
	fmt.Fprintln(out, promptLine(fmt.Sprintf("  %s%s◉  %s%s", cBold, cCyan, title, cReset)))
	fmt.Fprintln(out, boxSep())
	fmt.Fprintln(out, promptLine(""))
}

// promptOption draws one numbered choice: the number and its label on one row,
// and the explanation dimmed underneath.
func promptOption(out io.Writer, n int, label string, detail ...string) {
	fmt.Fprintln(out, promptLine(fmt.Sprintf("    %s[%d]%s  %s", cBold, n, cReset, label)))
	for _, line := range detail {
		if line == "" {
			continue
		}
		fmt.Fprintln(out, promptLine(fmt.Sprintf("         %s%s%s", cDim, line, cReset)))
	}
	fmt.Fprintln(out, promptLine(""))
}

// promptClose ends the frame and asks the question, returning what was typed.
// ok is false only when stdin ended without a line, which every caller reads as
// "leave it alone".
func promptClose(in *bufio.Scanner, out io.Writer, hint, question, def string) (string, bool) {
	fmt.Fprintln(out, boxBottom())
	fmt.Fprintln(out)
	if hint != "" {
		fmt.Fprintf(out, "    %s%s%s\n", cDim, hint, cReset)
	}
	fmt.Fprintf(out, "    %s [%s]: ", question, def)

	if !in.Scan() {
		fmt.Fprintln(out)
		return "", false
	}
	return strings.TrimSpace(in.Text()), true
}

// ── The questions ────────────────────────────────────────────────────────

// askLanguage puts the language question on screen and returns the chosen code.
//
// Asked in both languages, because it is the one question that cannot be asked
// in the answer to itself. Turkish leads: an English speaker reads "[2] English"
// regardless of what is above it, while a Turkish speaker facing an
// English-first screen has to work out that a Turkish option exists at all.
func askLanguage(in io.Reader, out io.Writer) string {
	scanner := bufio.NewScanner(in)

	promptHeading(out, turkish.langTitle)
	promptOption(out, 1, "Türkçe")
	promptOption(out, 2, "English")

	typed, ok := promptClose(scanner, out, "", turkish.langChoice, "1")
	if !ok {
		return "en"
	}
	if typed == "2" {
		return "en"
	}
	// Enter and anything unrecognized land on Turkish, which is what the [1]
	// in the prompt promises. A default that does not match the number shown is
	// a worse answer than either language.
	return "tr"
}

// ensureLanguage asks for a language the first time and remembers it.
//
// Only the first time: this is the one question whose right answer does not
// change, so asking it every start would be noise where the connection
// question is a check. `lang` in the console is how it changes afterwards.
func ensureLanguage(cfg *config.Config) language {
	if cfg.Language != "" {
		return languageByCode(cfg.Language)
	}

	cfg.Language = askLanguage(os.Stdin, os.Stdout)
	if err := config.Save(cfg); err != nil {
		// Not fatal, and not silent either: the question simply comes back next
		// start, which is a smaller problem than a monitor that will not run.
		log.Warnf("Failed to save the language choice: %v", err)
	}
	return languageByCode(cfg.Language)
}

// parseLanguageChoice reads an answer to the console's `lang` command. ok is
// false for a bare enter or anything unrecognized, both of which mean "leave it
// alone" — reading either as a choice would let someone who typed the command
// to see what it said change a setting by looking at it.
func parseLanguageChoice(typed string) (code string, ok bool) {
	switch strings.TrimSpace(typed) {
	case "1":
		return "tr", true
	case "2":
		return "en", true
	default:
		return "", false
	}
}
