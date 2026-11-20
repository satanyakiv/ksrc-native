// Package cache stores generated source trees under the user's cache
// directory.
//
// Layout:
//
//	<root>/<namespace>/<key>/meta.json   written last; its presence marks the entry ready
//	<root>/<namespace>/<key>/src/...     the searchable files
//
// An entry is built in a temporary sibling directory and renamed into place,
// so a reader never sees a half-written entry and two processes can build the
// same entry at once without harm.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// EnvDir is the environment variable that overrides the cache root.
const EnvDir = "KSRC_NATIVE_CACHE_DIR"

const (
	appDir   = "ksrc-native"
	metaFile = "meta.json"
	srcDir   = "src"
)

// Store is a cache rooted at one directory.
type Store struct {
	Root string
}

// Open returns the store at KSRC_NATIVE_CACHE_DIR when set, else at
// os.UserCacheDir()/ksrc-native. It does not create the directory.
func Open(getenv func(string) string) (Store, error) {
	if getenv != nil {
		if dir := getenv(EnvDir); dir != "" {
			abs, err := filepath.Abs(dir)
			if err != nil {
				return Store{}, errs.Wrap(err, errs.Cache, fmt.Sprintf("cannot resolve %s=%s", EnvDir, dir), "")
			}
			return Store{Root: abs}, nil
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return Store{}, errs.Wrap(err, errs.Cache, "cannot find the user cache directory: "+err.Error(), "set "+EnvDir+" to a writable directory")
	}
	return Store{Root: filepath.Join(base, appDir)}, nil
}

// Meta describes a cache entry. It is stored as meta.json.
type Meta struct {
	Coordinate string    `json:"coordinate"`       // for example kn.platform:UIKit:2.4.21
	Target     string    `json:"target,omitempty"` // for example ios_arm64
	Origin     string    `json:"origin"`           // where the data came from, for example the klib path
	Files      int       `json:"files"`            // number of files under src
	Created    time.Time `json:"created"`
}

// Entry is a ready cache entry.
type Entry struct {
	Dir  string // the entry directory
	Src  string // the directory with searchable files
	Meta Meta
}

// Key derives a stable entry key from the parts that identify the data.
func Key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Get returns the entry for a namespace and key when it is ready.
func (s Store) Get(namespace, key string) (Entry, bool) {
	return readEntry(filepath.Join(s.Root, namespace, key))
}

// Ensure returns the entry for a namespace and key, building it first when it
// is missing. build must fill srcDir and return the entry's metadata.
func (s Store) Ensure(namespace, key string, build func(srcDir string) (Meta, error)) (Entry, error) {
	if e, ok := s.Get(namespace, key); ok {
		return e, nil
	}

	parent := filepath.Join(s.Root, namespace)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Entry{}, cacheError(err, parent)
	}
	tmp, err := os.MkdirTemp(parent, ".tmp-")
	if err != nil {
		return Entry{}, cacheError(err, parent)
	}
	defer os.RemoveAll(tmp)

	tmpSrc := filepath.Join(tmp, srcDir)
	if err := os.MkdirAll(tmpSrc, 0o755); err != nil {
		return Entry{}, cacheError(err, tmp)
	}
	meta, err := build(tmpSrc)
	if err != nil {
		return Entry{}, err
	}
	if meta.Created.IsZero() {
		meta.Created = time.Now().UTC().Truncate(time.Second)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return Entry{}, cacheError(err, tmp)
	}
	if err := os.WriteFile(filepath.Join(tmp, metaFile), append(data, '\n'), 0o644); err != nil {
		return Entry{}, cacheError(err, tmp)
	}

	dir := filepath.Join(parent, key)
	if err := os.Rename(tmp, dir); err != nil {
		// Another process may have finished the same entry first.
		if e, ok := readEntry(dir); ok {
			return e, nil
		}
		// A directory without meta.json is a leftover; replace it.
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return Entry{}, cacheError(rmErr, dir)
		}
		if err := os.Rename(tmp, dir); err != nil {
			return Entry{}, cacheError(err, dir)
		}
	}
	e, ok := readEntry(dir)
	if !ok {
		return Entry{}, cacheError(errors.New("entry is not readable after it was written"), dir)
	}
	return e, nil
}

// List returns the ready entries of a namespace, sorted by coordinate and
// target.
func (s Store) List(namespace string) []Entry {
	dirs, err := os.ReadDir(filepath.Join(s.Root, namespace))
	if err != nil {
		return nil
	}
	var entries []Entry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if e, ok := readEntry(filepath.Join(s.Root, namespace, d.Name())); ok {
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].Meta, entries[j].Meta
		if a.Coordinate != b.Coordinate {
			return a.Coordinate < b.Coordinate
		}
		return a.Target < b.Target
	})
	return entries
}

// Size returns the total size in bytes of the regular files in the cache. A
// cache that does not exist yet has size zero.
func (s Store) Size() (int64, error) {
	if _, err := os.Stat(s.Root); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	return dirSize(s.Root), nil
}

// Usage describes the disk use of one namespace.
type Usage struct {
	Namespace string
	Entries   []EntryUsage // ready entries, sorted like List
	Size      int64        // all bytes of the namespace, leftovers of interrupted builds included
}

// EntryUsage is a ready entry with its size on disk.
type EntryUsage struct {
	Entry
	Size int64
}

// Usage measures the given namespaces. A namespace that does not exist has
// no entries and size zero.
func (s Store) Usage(namespaces ...string) []Usage {
	out := make([]Usage, 0, len(namespaces))
	for _, ns := range namespaces {
		u := Usage{Namespace: ns, Size: dirSize(filepath.Join(s.Root, ns))}
		for _, e := range s.List(ns) {
			u.Entries = append(u.Entries, EntryUsage{Entry: e, Size: dirSize(e.Dir)})
		}
		out = append(out, u)
	}
	return out
}

// Remove deletes the given namespaces with everything in them. Only the
// named directories below the root are touched, so a cache root that points
// at a shared directory by mistake loses nothing else.
func (s Store) Remove(namespaces ...string) error {
	for _, ns := range namespaces {
		if ns == "" || ns != filepath.Base(ns) || ns == "." || ns == ".." {
			return cacheError(errors.New("invalid namespace "+ns), s.Root)
		}
		dir := filepath.Join(s.Root, ns)
		if err := os.RemoveAll(dir); err != nil {
			return cacheError(err, dir)
		}
	}
	return nil
}

func dirSize(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func readEntry(dir string) (Entry, bool) {
	data, err := os.ReadFile(filepath.Join(dir, metaFile))
	if err != nil {
		return Entry{}, false
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Entry{}, false
	}
	return Entry{Dir: dir, Src: filepath.Join(dir, srcDir), Meta: meta}, true
}

func cacheError(err error, path string) error {
	return errs.Wrap(err, errs.Cache,
		fmt.Sprintf("cannot write the cache at %s: %v", path, err),
		"set "+EnvDir+" to a writable directory")
}
