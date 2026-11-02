package common

import (
	"os"
	"time"

	"github.com/briandowns/spinner"
	"golang.org/x/term"
)

// interactive reports whether stdout is a terminal. The operations these spinners
// wrap also run inside ragd, where every animation frame would land in the
// daemon's journal; there the spinner is skipped entirely.
func interactive() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Interactive reports whether an operator can be asked a question: stdout is a terminal.
// A decision that must never be defaulted — accepting a shortfall, for instance — fails
// instead when this is false.
func Interactive() bool { return interactive() }

func StartProgressSpinner(prefix string) (stop func()) {
	if !interactive() {
		return func() {}
	}

	s := spinner.New(spinner.CharSets[9], time.Millisecond*200)
	s.Prefix = prefix + " "
	s.Start()

	return s.Stop
}

// StartUpdatableSpinner starts a spinner whose prefix can be changed while it
// runs (e.g. to show live operation progress). It returns an update function to
// set a new prefix and a stop function to halt the spinner.
func StartUpdatableSpinner(prefix string) (update func(string), stop func()) {
	if !interactive() {
		return func(string) {}, func() {}
	}

	s := spinner.New(spinner.CharSets[9], time.Millisecond*200)
	s.Prefix = prefix + " "
	s.Start()

	return func(p string) { s.Prefix = p + " " }, s.Stop
}
