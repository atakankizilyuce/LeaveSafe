package main

import (
	"bytes"
	"github.com/leavesafe/leavesafe/internal/ws"
	"strings"
	"testing"
)

func TestLocalizedTerminalMessages(t *testing.T) {
	var out bytes.Buffer
	sb := &statusBar{out: &out, language: "tr", handedBack: true}
	sb.writeLine("  Already disarmed")
	if !strings.Contains(out.String(), "Koruma zaten kapalı") {
		t.Fatal(out.String())
	}
	if got := sensorMark(ws.SensorInfo{Available: true, Enabled: true}, "tr"); got != "açık" {
		t.Fatal(got)
	}
	if got := sensorMark(ws.SensorInfo{}, "tr"); got != "kullanılamıyor" {
		t.Fatal(got)
	}
	if got := sensorMark(ws.SensorInfo{Available: true, Failure: "driver"}, "tr"); got != "ÇALIŞMIYOR — driver" {
		t.Fatal(got)
	}
	if got := sensorMark(ws.SensorInfo{Available: true}, "tr"); got != "kapalı" {
		t.Fatal(got)
	}
}
func TestTerminalLanguageCodeChoices(t *testing.T) {
	for input, want := range map[string]string{"tr": "tr", "en-US": "en", "1": "tr", "2": "en"} {
		if got, ok := parseLanguageChoice(input); !ok || got != want {
			t.Errorf("%q => %q,%v", input, got, ok)
		}
	}
	if _, ok := parseLanguageChoice("999"); ok {
		t.Fatal("invalid choice accepted")
	}
}
func TestTerminalUnicodeColumns(t *testing.T) {
	for input, want := range map[string]int{"中文": 4, "粵語": 4, "日本語": 6, "e\u0301": 1, "عَرَبِي": 4, "हिन्दी": 3, "Ａ": 2, "가": 2, "👋": 2, "\u200d": 0, "\x7f": 0, "\u303f": 1} {
		if got := visLen(input); got != want {
			t.Errorf("%q width=%d want%d", input, got, want)
		}
	}
	if got := truncateVisible("中文A", 3); got != "中" {
		t.Fatal(got)
	}
	if got := truncateVisible("e\u0301x", 1); got != "e\u0301" {
		t.Fatal(got)
	}
	if got := truncateVisible(cRed+"中文"+cReset, 3); visLen(got) != 2 || !strings.HasSuffix(got, cReset) {
		t.Fatal(got)
	}
}
