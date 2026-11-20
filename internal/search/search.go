// Package search runs ripgrep over resolved source roots and maps its JSON
// output to file-ids.
package search

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
)

// Root is one directory to search. A hit in <Dir>/a/b.kt gets the file-id
// Prefix + "a/b.kt".
type Root struct {
	Dir    string
	Prefix string // for example "kn.platform:UIKit:2.4.21!/"
}

// Options describe one search.
type Options struct {
	Pattern   string
	Roots     []Root
	Context   int      // lines of context around each match; 0 for none
	ExtraArgs []string // passed to rg before the pattern
}

// Line is one line of output: a match or a context line.
type Line struct {
	FileID string
	Number int    // 1-based line number
	Column int    // 1-based byte column of the first match; 0 for context lines
	Text   string // the line without its line terminator
	Match  bool
}

const rgInstallHint = "install ripgrep (macOS: brew install ripgrep; other systems: https://github.com/BurntSushi/ripgrep#installation)"

// FindRipgrep locates the rg binary.
func FindRipgrep(runner executil.Runner) (string, error) {
	path, err := runner.LookPath("rg")
	if err != nil {
		return "", errs.New(errs.NoRipgrep, "the rg binary is not on PATH", rgInstallHint)
	}
	return path, nil
}

// Run searches the roots and calls emit for every match and context line, in
// rg's order. It returns the number of matching lines. No matches is not an
// error.
func Run(ctx context.Context, runner executil.Runner, opts Options, emit func(Line) error) (int, error) {
	if opts.Pattern == "" {
		return 0, errs.New(errs.BadArgs, "the search pattern is empty", "pass a regular expression, for example 'class platform/UIKit/UIApplication'")
	}
	if len(opts.Roots) == 0 {
		return 0, nil
	}
	rg, err := FindRipgrep(runner)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pr, pw := io.Pipe()
	runErr := make(chan error, 1)
	go func() {
		err := runner.Run(ctx, executil.Command{Path: rg, Args: rgArgs(opts), Stdout: pw})
		pw.Close()
		runErr <- err
	}()

	matches, parseErr := parse(pr, opts.Roots, emit)
	if parseErr != nil {
		// Stop rg and unblock its writer before waiting for it.
		cancel()
		pr.CloseWithError(parseErr)
		<-runErr
		return matches, parseErr
	}

	switch err := <-runErr; executil.ExitCode(err) {
	case 1:
		// rg exits with 1 when nothing matched.
		return matches, nil
	default:
		if err != nil {
			return matches, errs.Wrap(err, errs.RgFailed, "rg failed: "+rgMessage(err),
				"check the pattern (a Rust regular expression; pass `-- -F` for a literal string) and the rg arguments after `--`")
		}
	}
	return matches, nil
}

// rgArgs builds the rg command line. Ignore files and user configuration are
// switched off: the cache may sit below a directory with a .gitignore, and the
// output must not depend on the machine.
func rgArgs(opts Options) []string {
	args := []string{"--json", "--no-config", "--no-ignore", "--sort", "path"}
	if opts.Context > 0 {
		args = append(args, "--context", strconv.Itoa(opts.Context))
	}
	args = append(args, opts.ExtraArgs...)
	args = append(args, "--regexp", opts.Pattern, "--")
	for _, r := range opts.Roots {
		args = append(args, r.Dir)
	}
	return args
}

func rgMessage(err error) string {
	var msg string
	var exit *executil.ExitError
	if errors.As(err, &exit) {
		msg = strings.TrimSpace(exit.Stderr)
	}
	if msg == "" {
		msg = err.Error()
	}
	return strings.Join(strings.Fields(msg), " ")
}

// message is one object of rg's --json stream.
type message struct {
	Type string `json:"type"`
	Data struct {
		Path       text `json:"path"`
		Lines      text `json:"lines"`
		LineNumber int  `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

// text is rg's representation of arbitrary data: "text" when it is valid
// UTF-8, base64 "bytes" otherwise.
type text struct {
	Text  *string `json:"text"`
	Bytes *string `json:"bytes"`
}

func (t text) String() string {
	if t.Text != nil {
		return *t.Text
	}
	if t.Bytes != nil {
		if raw, err := base64.StdEncoding.DecodeString(*t.Bytes); err == nil {
			return string(raw)
		}
	}
	return ""
}

func parse(r io.Reader, roots []Root, emit func(Line) error) (int, error) {
	matches := 0
	reader := bufio.NewReaderSize(r, 256<<10)
	for {
		raw, readErr := reader.ReadBytes('\n')
		if len(strings.TrimSpace(string(raw))) > 0 {
			var m message
			if err := json.Unmarshal(raw, &m); err != nil {
				return matches, errs.New(errs.BadArgs,
					"rg printed output that is not JSON, so an extra rg argument changed the output mode",
					"drop output-mode flags such as -l, -c, --files or --vimgrep from the arguments after `--`")
			}
			if m.Type == "match" || m.Type == "context" {
				n, err := emitLines(m, roots, emit)
				matches += n
				if err != nil {
					return matches, err
				}
			}
		}
		if readErr == io.EOF {
			return matches, nil
		}
		if readErr != nil {
			return matches, readErr
		}
	}
}

// emitLines emits the lines of one message. A multi-line match yields one
// output line per source line.
func emitLines(m message, roots []Root, emit func(Line) error) (int, error) {
	fileID := fileIDFor(m.Data.Path.String(), roots)
	isMatch := m.Type == "match"
	column := 0
	if isMatch {
		column = 1
		if len(m.Data.Submatches) > 0 {
			column = m.Data.Submatches[0].Start + 1
		}
	}

	body := strings.TrimSuffix(m.Data.Lines.String(), "\n")
	count := 0
	for i, line := range strings.Split(body, "\n") {
		out := Line{
			FileID: fileID,
			Number: m.Data.LineNumber + i,
			Text:   strings.TrimSuffix(line, "\r"),
			Match:  isMatch,
		}
		if isMatch {
			out.Column = 1
			if i == 0 {
				out.Column = column
			}
			count++
		}
		if err := emit(out); err != nil {
			return count, err
		}
	}
	return count, nil
}

// fileIDFor maps an absolute path printed by rg to a file-id. A path outside
// every root is returned as is, which should not happen.
func fileIDFor(path string, roots []Root) string {
	for _, r := range roots {
		rel, err := filepath.Rel(r.Dir, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		return r.Prefix + filepath.ToSlash(rel)
	}
	return path
}

// Format renders a line in the output contract:
//
//	<file-id> <line>:<col>:<text>   for a match
//	<file-id> <line>-<text>         for a context line
func Format(l Line) string {
	if l.Match {
		return fmt.Sprintf("%s %d:%d:%s", l.FileID, l.Number, l.Column, l.Text)
	}
	return fmt.Sprintf("%s %d-%s", l.FileID, l.Number, l.Text)
}
