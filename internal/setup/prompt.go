// Package setup implements the interactive 'configure' command and the
// annotated 'show-config' command.
package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// errEnded is returned when input ends while an answer is still needed.
var errEnded = errors.New("input ended before configuration was complete")

type prompter struct {
	in  *bufio.Reader
	out io.Writer
}

func (p *prompter) say(format string, a ...any) {
	fmt.Fprintf(p.out, format, a...)
}

// ask shows label and the default, and returns the trimmed answer. Enter
// accepts the default. End of input also accepts the default; if there is no
// default to fall back on, it is an error so scripted input cannot hang.
func (p *prompter) ask(label, def string) (string, error) {
	if def != "" {
		p.say("%s [%s]: ", label, def)
	} else {
		p.say("%s: ", label)
	}
	line, err := p.in.ReadString('\n')
	line = strings.TrimSpace(line)
	if err != nil && line == "" {
		if def == "" {
			return "", errEnded
		}
		p.say("%s\n", def)
		return def, nil
	}
	if line == "" {
		return def, nil
	}
	return line, nil
}

// askValid repeats ask until check accepts the answer.
func (p *prompter) askValid(label, def string, check func(string) error) (string, error) {
	for {
		v, err := p.ask(label, def)
		if err != nil {
			return "", err
		}
		if cerr := check(v); cerr != nil {
			p.say("  %v\n", cerr)
			if v == def {
				// The default itself is invalid; there is nothing to fall back on.
				if def == "" {
					continue
				}
				def = ""
			}
			continue
		}
		return v, nil
	}
}

func (p *prompter) askInt(label string, def, min int) (int, error) {
	v, err := p.askValid(label, strconv.Itoa(def), func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil || n < min {
			return fmt.Errorf("enter a whole number of at least %d", min)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}

func (p *prompter) askFloat(label string, def float64) (float64, error) {
	v, err := p.askValid(label, strconv.FormatFloat(def, 'f', -1, 64), func(s string) error {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || n <= 0 {
			return errors.New("enter a number greater than zero")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(v, 64)
}

func (p *prompter) askYesNo(label string, def bool) (bool, error) {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	for {
		v, err := p.ask(label, d)
		if err != nil {
			return false, err
		}
		if v == d {
			return def, nil
		}
		switch strings.ToLower(v) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		p.say("  answer y or n\n")
	}
}
