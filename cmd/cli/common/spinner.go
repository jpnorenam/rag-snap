package common

import (
	"time"

	"github.com/briandowns/spinner"
)

func StartProgressSpinner(prefix string) (stop func()) {
	s := spinner.New(spinner.CharSets[9], time.Millisecond*200)
	s.Prefix = prefix + " "
	s.Start()

	return s.Stop
}

// StartUpdatableSpinner starts a spinner whose prefix can be changed while it
// runs (e.g. to show live operation progress). It returns an update function to
// set a new prefix and a stop function to halt the spinner.
func StartUpdatableSpinner(prefix string) (update func(string), stop func()) {
	s := spinner.New(spinner.CharSets[9], time.Millisecond*200)
	s.Prefix = prefix + " "
	s.Start()

	return func(p string) { s.Prefix = p + " " }, s.Stop
}
