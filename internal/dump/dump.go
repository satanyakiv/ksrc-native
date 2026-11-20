// Package dump splits the text printed by `klib dump-metadata` into one file
// per class, so the result can be searched and read like library sources.
//
// The dump of a platform klib looks like this (Kotlin 2.4):
//
//	library {
//	  // module name: <org.jetbrains.kotlin.native.platform.UIKit>
//
//	  library fragment {
//	    // package name: platform.UIKit
//
//	    // class name: platform/UIKit/UIApplication
//	    // class name: platform/UIKit/UIApplication.Companion
//
//	    @kotlinx/cinterop/ExternalObjCClass
//	    public open class platform/UIKit/UIApplication : platform/UIKit/UIResponder {
//
//	      public open external fun delegate(): platform/UIKit/UIApplicationDelegateProtocol?
//	    }
//
//	    package {
//
//	      public typealias UIAccessibilityTraits = kotlin/ULong
//	    }
//	  }
//	}
//
// Classes sit at four spaces of indent inside a fragment, with their
// annotations on the lines before the header. Top-level functions, properties
// and type aliases sit at six spaces inside `package { }` blocks. One package
// is spread over several fragments.
//
// Split writes every class into "<OutermostClass>.kt", so a nested class or a
// companion object lands in the file of its outer class, and collects all
// `package { }` content in "_toplevel.kt". Lines are shifted left, so a class
// header and a top-level declaration both start in column one.
package dump

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// FormatVersion names what Split writes for a given dump: the file names
// and the text of the files. It is part of the cache key of a platform
// library. Change it with every change of the output, and the cached dumps
// are rebuilt on their next use instead of serving the old files.
const FormatVersion = "1"

// TopLevelFile holds the functions, properties and type aliases of the
// library that belong to no class.
const TopLevelFile = "_toplevel.kt"

// FileSuffix is appended to a class name to form its file name. The content is
// a metadata dump, not compilable Kotlin; the suffix gives editors and rg type
// filters something to work with.
const FileSuffix = ".kt"

const (
	classIndent   = 4 // indent of class headers inside a fragment
	packageIndent = 6 // indent of declarations inside a `package { }` block

	fragmentOpen  = "  library fragment {"
	fragmentClose = "  }"
	packageOpen   = "    package {"
	blockClose    = "    }"

	moduleMarker  = "// module name: "
	packageMarker = "    // package name: "
	classMarker   = "    // class name: "
)

// File is one file produced by Split.
type File struct {
	Name    string // file name without directories, for example UIApplication.kt
	Content []byte
}

// Result is a split dump.
type Result struct {
	Module   string   // unique name of the library, for example org.jetbrains.kotlin.native.platform.UIKit
	Packages []string // package names in order of first appearance, without empty ones
	Files    []File   // class files in order of first appearance; TopLevelFile is last when present
	Classes  int      // number of class, interface and object declarations
}

// classHeader finds the qualified name in a class header line such as
// "public final companion object platform/UIKit/UIView.Companion : ... {".
var classHeader = regexp.MustCompile(`(?:^|\s)(?:class|interface|object)\s+([^\s:<{(]+)`)

type state int

const (
	outside    state = iota // not inside a library fragment
	inFragment              // inside a fragment, between declarations
	inClass                 // inside a class body
	inPackage               // inside a `package { }` block
)

// Split reads a metadata dump and groups its declarations into files.
func Split(r io.Reader) (*Result, error) {
	s := &splitter{
		result:  &Result{},
		files:   make(map[string]*fileBuffer),
		byLower: make(map[string]string),
	}
	reader := bufio.NewReaderSize(r, 256<<10)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			s.line(strings.TrimRight(line, "\r\n"))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return s.finish()
}

type fileBuffer struct {
	buf          bytes.Buffer
	pendingBlank bool // a blank line is owed before the next written line
}

func (f *fileBuffer) write(line string) {
	if line == "" {
		f.pendingBlank = f.buf.Len() > 0
		return
	}
	if f.pendingBlank {
		f.buf.WriteByte('\n')
		f.pendingBlank = false
	}
	f.buf.WriteString(line)
	f.buf.WriteByte('\n')
}

type splitter struct {
	result *Result

	state     state
	fragments int
	unclosed  int         // blocks that were still open where they had to be closed
	pending   []string    // annotation lines that wait for their class header
	current   *fileBuffer // the file that receives the open block

	files   map[string]*fileBuffer
	order   []string          // class file names in order of first appearance
	byLower map[string]string // lower-case file name -> name in use
	seenPkg map[string]bool
}

func (s *splitter) line(line string) {
	// A class or package block ends with one exact line. A fragment marker
	// while such a block is open means that line did not come: the dump is
	// laid out differently from what this splitter knows. Without this check
	// the rest of the library would silently land in one file. The state is
	// reset, so every such place is counted, and finish reports them.
	if (s.state == inClass || s.state == inPackage) && (line == fragmentOpen || line == fragmentClose) {
		s.unclosed++
		s.state = outside
		if line == fragmentOpen {
			s.fragments++
			s.state = inFragment
		}
		return
	}
	switch s.state {
	case outside:
		s.lineOutside(line)
	case inFragment:
		s.lineInFragment(line)
	case inClass:
		s.current.write(dedent(line, classIndent))
		if line == blockClose {
			s.state = inFragment
		}
	case inPackage:
		if line == blockClose {
			s.state = inFragment
			return
		}
		s.current.write(dedent(line, packageIndent))
	}
}

func (s *splitter) lineOutside(line string) {
	switch {
	case line == fragmentOpen:
		s.fragments++
		s.state = inFragment
	case s.result.Module == "":
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), moduleMarker); ok {
			s.result.Module = strings.Trim(name, "<>")
		}
	}
}

func (s *splitter) lineInFragment(line string) {
	switch {
	case line == fragmentClose:
		s.flushPendingToTopLevel()
		s.state = outside
	case strings.TrimSpace(line) == "":
		// Blank lines between declarations carry no information.
	case strings.HasPrefix(line, packageMarker):
		s.addPackage(strings.TrimPrefix(line, packageMarker))
	case strings.HasPrefix(line, classMarker):
		// The index of class names repeats what the headers say.
	case line == packageOpen:
		s.flushPendingToTopLevel()
		s.current = s.file(TopLevelFile)
		s.current.write("")
		s.state = inPackage
	case strings.HasPrefix(line, "    @"):
		s.pending = append(s.pending, line)
	default:
		s.declaration(line)
	}
}

// declaration handles a line at class indent that is neither a marker nor an
// annotation: normally a class header.
func (s *splitter) declaration(line string) {
	name, ok := classFileName(line)
	if !ok {
		// Unknown shape: keep the text searchable instead of dropping it.
		s.pending = append(s.pending, line)
		s.flushPendingToTopLevel()
		return
	}

	s.result.Classes++
	s.current = s.file(name)
	s.current.write("")
	for _, annotation := range s.pending {
		s.current.write(dedent(annotation, classIndent))
	}
	s.pending = nil
	s.current.write(dedent(line, classIndent))
	if strings.HasSuffix(line, "{") {
		s.state = inClass
	}
}

func (s *splitter) flushPendingToTopLevel() {
	if len(s.pending) == 0 {
		return
	}
	f := s.file(TopLevelFile)
	f.write("")
	for _, line := range s.pending {
		f.write(dedent(line, classIndent))
	}
	s.pending = nil
}

func (s *splitter) addPackage(name string) {
	name = strings.TrimSpace(name)
	if name == "" || s.seenPkg[name] {
		return
	}
	if s.seenPkg == nil {
		s.seenPkg = make(map[string]bool)
	}
	s.seenPkg[name] = true
	s.result.Packages = append(s.result.Packages, name)
}

// file returns the buffer of a file. Names that differ only by case share one
// file, because the default macOS file system cannot hold both.
func (s *splitter) file(name string) *fileBuffer {
	lower := strings.ToLower(name)
	if used, ok := s.byLower[lower]; ok {
		return s.files[used]
	}
	f := &fileBuffer{}
	s.byLower[lower] = name
	s.files[name] = f
	if name != TopLevelFile {
		s.order = append(s.order, name)
	}
	return f
}

func (s *splitter) finish() (*Result, error) {
	if s.fragments == 0 {
		return nil, errs.New(errs.DumpFormat,
			"the klib output has no `library fragment` blocks, so it is not a metadata dump this version understands",
			"run `ksrc-native doctor`; if the Kotlin version is newer than this tool, report the version in an issue")
	}
	if s.state != outside {
		// The dump ends inside a fragment: it was cut, or the fragment ends
		// with another line than expected.
		s.unclosed++
	}
	if s.unclosed > 0 {
		times := "1 time"
		if s.unclosed > 1 {
			times = fmt.Sprintf("%d times", s.unclosed)
		}
		return nil, errs.New(errs.DumpFormat,
			fmt.Sprintf("a class, package or fragment block of the klib output does not end where expected (%s), so it is not a metadata dump this version understands", times),
			"run `ksrc-native doctor`; if the Kotlin version is newer than this tool, report the version in an issue")
	}
	s.flushPendingToTopLevel()

	for _, name := range s.order {
		s.result.Files = append(s.result.Files, File{Name: name, Content: s.files[name].buf.Bytes()})
	}
	if top, ok := s.files[TopLevelFile]; ok && top.buf.Len() > 0 {
		s.result.Files = append(s.result.Files, File{Name: TopLevelFile, Content: top.buf.Bytes()})
	}
	return s.result, nil
}

// classFileName maps a class header to the file of its outermost class:
// "... object platform/UIKit/UIView.Companion : ... {" -> "UIView.kt".
func classFileName(header string) (string, bool) {
	m := classHeader.FindStringSubmatch(header)
	if m == nil {
		return "", false
	}
	qualified := m[1]
	if i := strings.LastIndexByte(qualified, '/'); i >= 0 {
		qualified = qualified[i+1:]
	}
	outer, _, _ := strings.Cut(qualified, ".")
	outer = sanitize(outer)
	if outer == "" {
		return "", false
	}
	return shorten(outer) + FileSuffix, true
}

// maxStemBytes is the longest class name used as a file name as it is. A
// file name holds 255 bytes on APFS and ext4; the rest is room for the hash
// and the suffix.
const maxStemBytes = 200

// shorten cuts a sanitized class name that is too long for a file name and
// appends 8 hex digits of its hash, so two long names with the same start
// get two files. The hash ignores case, like the rule that names which
// differ only by case share a file. The full name stays in the content.
func shorten(stem string) string {
	if len(stem) <= maxStemBytes {
		return stem
	}
	sum := sha256.Sum256([]byte(strings.ToLower(stem)))
	return stem[:maxStemBytes] + "-" + hex.EncodeToString(sum[:4])
}

// sanitize keeps the characters that are safe in a file name on every
// supported file system and replaces the rest.
func sanitize(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '$', r == '-':
			return r
		default:
			return '_'
		}
	}, name)
}

// dedent removes up to n leading spaces.
func dedent(line string, n int) string {
	i := 0
	for i < n && i < len(line) && line[i] == ' ' {
		i++
	}
	return line[i:]
}

// WriteTo writes the files into dir, creating it when needed.
func (r *Result) WriteTo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range r.Files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0o644); err != nil {
			return err
		}
	}
	return nil
}
