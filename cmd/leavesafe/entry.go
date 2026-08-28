// The process entry point, and nothing else.
//
// It is a file of its own, and named in sonar-project.properties as excluded
// from coverage, because no test can reach it: main is what the operating
// system calls, it ends by serving until the program is killed, and a test that
// called it would never return to assert anything. Every decision it makes was
// moved out to somewhere that can be — planTerminal, loadConfig,
// ensureLanguageChoice, startApp — so what is left here is the wiring between
// them, and that wiring is what an e2e run exercises by starting the real
// binary.
//
// Excluded from coverage only, not from analysis: every rule still reads this
// file, and a bug in it still fails the quality gate.

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/logsink"

	"github.com/leavesafe/leavesafe/internal/safe"
)

func main() {
	devMode := flag.Bool("dev", false, "serve web assets from filesystem for live reload")
	showVersion := flag.Bool("version", false, "print the version and exit")
	headless := flag.Bool("headless", false, "run without the terminal dashboard, for autostart")
	plain := flag.Bool("plain", false, "log to the terminal instead of drawing the full-screen dashboard")
	managed := flag.Bool("managed", false, "started by the LeaveSafe desktop application, which owns this copy's version")
	flag.Usage = printUsage
	flag.Parse()

	if *showVersion {
		fmt.Println(versionLine())
		return
	}

	// Subcommands run and exit without starting the monitor: they administer
	// the installation rather than being part of it.
	if args := flag.Args(); len(args) > 0 {
		os.Exit(runSubcommand(args))
	}

	log.SetFormatter(consoleLogFormatter())
	// Between the logger and its output from here on, and before anything else
	// is started. logrus holds one mutex across the write to Out, so an output
	// that blocks stops every log line in the process — and the first statement
	// in each sensor loop is a log line. See internal/logsink for the afternoon
	// that taught us that.
	log.SetOutput(logsink.New(os.Stderr, 0))

	plainRun, _ := planTerminal(*headless, *plain, os.Stdout)

	cfg := loadConfig()
	ensureLanguageChoice(cfg, *headless)

	a, err := startApp(appOptions{
		cfg:      cfg,
		headless: *headless,
		plain:    plainRun,
		devMode:  *devMode,
		managed:  *managed,
		out:      os.Stdout,
		in:       os.Stdin,
	})
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer a.close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	safe.Go("shutdown", func() {
		<-sigCh
		a.shutdown()
		os.Exit(0)
	})

	if err := a.serve(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
