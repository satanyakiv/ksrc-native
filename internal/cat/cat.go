// Package cat prints a file or a range of its lines.
package cat

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// Range selects lines, 1-based and inclusive. The zero value selects the whole
// file; End == 0 means "to the end of the file".
type Range struct {
	Start int
	End   int
}

const linesHint = "use --lines A,B with 1-based line numbers, for example --lines 1,40"

// ParseLines parses the value of --lines: "A,B", "A," (from A to the end) or
// "A" (one line). "A-B" and "A:B" are accepted too. An empty value selects
// the whole file.
func ParseLines(s string) (Range, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Range{}, nil
	}
	bad := func() (Range, error) {
		return Range{}, errs.New(errs.BadLines, fmt.Sprintf("cannot parse the line range %q", s), linesHint)
	}

	startText, endText, isRange := s, "", false
	if i := strings.IndexAny(s, ",-:"); i >= 0 {
		startText, endText, isRange = s[:i], s[i+1:], true
	}
	start, err := strconv.Atoi(strings.TrimSpace(startText))
	if err != nil || start < 1 {
		return bad()
	}
	if !isRange {
		return Range{Start: start, End: start}, nil
	}
	if strings.TrimSpace(endText) == "" {
		return Range{Start: start}, nil
	}
	end, err := strconv.Atoi(strings.TrimSpace(endText))
	if err != nil || end < start {
		return bad()
	}
	return Range{Start: start, End: end}, nil
}

// Print writes the selected lines of the file at path to w. With numbers set,
// every line is prefixed with its line number and a tab.
func Print(w io.Writer, path string, r Range, numbers bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	out := bufio.NewWriter(w)
	reader := bufio.NewReaderSize(f, 256<<10)
	lineNo, printed := 0, 0
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			lineNo++
			if lineNo >= r.Start && (r.End == 0 || lineNo <= r.End) {
				if numbers {
					fmt.Fprintf(out, "%d\t", lineNo)
				}
				out.WriteString(strings.TrimRight(line, "\r\n"))
				out.WriteByte('\n')
				printed++
			}
		}
		if readErr == io.EOF || (r.End != 0 && lineNo >= r.End) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := out.Flush(); err != nil {
		return err
	}
	if printed == 0 && r.Start > 1 {
		return errs.New(errs.BadLines,
			fmt.Sprintf("the range starts at line %d, but the file has %d lines", r.Start, lineNo),
			fmt.Sprintf("use a range within 1,%d", lineNo))
	}
	return nil
}
