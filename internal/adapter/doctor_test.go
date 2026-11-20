package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// doctorLine returns the line of one part of the doctor output.
func doctorLine(t *testing.T, out, part string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, part+": ") {
			return line
		}
	}
	t.Fatalf("the doctor output has no %s line:\n%s", part, out)
	return ""
}

func TestDoctorChecksTheJVM(t *testing.T) {
	ctx := context.Background()

	t.Run("java on PATH that runs", func(t *testing.T) {
		w := newWorld(t)
		if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
			t.Fatalf("Doctor() error = %v\n%s", err, w.stdout.String())
		}
		out := w.stdout.String()
		if got, want := doctorLine(t, out, "java"), "java: ok - /ksrc-native-fake-bin/java ("+testutil.JavaVersionLine+")"; got != want {
			t.Errorf("java line = %q, want %q", got, want)
		}
		// The line sits between klib and platform: klib needs java, and the
		// platform libraries need klib.
		klib, java, platform := strings.Index(out, "\nklib: "), strings.Index(out, "\njava: "), strings.Index(out, "\nplatform: ")
		if !(klib < java && java < platform) {
			t.Errorf("the java line is not between klib and platform:\n%s", out)
		}
	})

	t.Run("JAVA_HOME wins over PATH", func(t *testing.T) {
		w := newWorld(t)
		javaHome := t.TempDir()
		java := filepath.Join(javaHome, "bin", "java")
		testutil.WriteFile(t, java, []byte("#!/bin/sh\necho 'openjdk version \"21.0.1\"' >&2\n"), 0o755)
		w.env["JAVA_HOME"] = javaHome
		if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
			t.Fatalf("Doctor() error = %v\n%s", err, w.stdout.String())
		}
		if got, want := doctorLine(t, w.stdout.String(), "java"), "java: ok - "+java+" (openjdk version \"21.0.1\")"; got != want {
			t.Errorf("java line = %q, want %q", got, want)
		}
	})

	t.Run("no java", func(t *testing.T) {
		w := newWorld(t)
		delete(w.runner.Fakes, "java")
		w.runner.Hidden = []string{"java"}
		err := w.a.Doctor(ctx, DoctorRequest{})
		if errs.Code(err) != DoctorFailed || !strings.Contains(err.Error(), "1 of the checks") {
			t.Errorf("Doctor() error = %v, want %s for one failed check", err, DoctorFailed)
		}
		line := doctorLine(t, w.stdout.String(), "java")
		if !strings.HasPrefix(line, "java: MISSING - no java on PATH and JAVA_HOME is not set") || !strings.Contains(line, "kn.platform") || !strings.Contains(line, "JAVA_HOME") {
			t.Errorf("java line = %q, want MISSING that says what breaks and what to do", line)
		}
	})

	t.Run("a java that does not run", func(t *testing.T) {
		w := newWorld(t)
		w.runner.Fakes["java"] = testutil.NoJavaRuntime
		err := w.a.Doctor(ctx, DoctorRequest{})
		if errs.Code(err) != DoctorFailed {
			t.Errorf("Doctor() error = %v, want %s", err, DoctorFailed)
		}
		line := doctorLine(t, w.stdout.String(), "java")
		if !strings.HasPrefix(line, "java: MISSING - /ksrc-native-fake-bin/java does not run: ") || !strings.Contains(line, "Unable to locate a Java Runtime") {
			t.Errorf("java line = %q, want MISSING with what java printed", line)
		}
	})

	t.Run("no distribution: java is not needed", func(t *testing.T) {
		w := newWorld(t)
		w.a.UserHome = t.TempDir()
		delete(w.runner.Fakes, "java")
		w.runner.Hidden = []string{"java"}
		err := w.a.Doctor(ctx, DoctorRequest{})
		// kotlin-native, klib and platform fail; java does not add a fourth.
		if errs.Code(err) != DoctorFailed || !strings.Contains(err.Error(), "3 of the checks") {
			t.Errorf("Doctor() error = %v, want %s for 3 failed checks", err, DoctorFailed)
		}
		if got, want := doctorLine(t, w.stdout.String(), "java"), "java: unavailable - needs a Kotlin/Native distribution"; got != want {
			t.Errorf("java line = %q, want %q", got, want)
		}
	})
}

func TestDoctorReportsACacheThatCannotBeUsed(t *testing.T) {
	ctx := context.Background()

	t.Run("a file is in the way", func(t *testing.T) {
		w := newWorld(t)
		file := filepath.Join(t.TempDir(), "not-a-directory")
		testutil.WriteFile(t, file, []byte("x"), 0o644)
		w.env[cache.EnvDir] = file

		err := w.a.Doctor(ctx, DoctorRequest{})
		if errs.Code(err) != DoctorFailed {
			t.Errorf("Doctor() error = %v, want %s", err, DoctorFailed)
		}
		line := doctorLine(t, w.stdout.String(), "cache")
		if !strings.HasPrefix(line, "cache: MISSING - E_CACHE: ") || !strings.Contains(line, "a file is in the way at "+file) || !strings.Contains(line, cache.EnvDir) {
			t.Errorf("cache line = %q, want MISSING with the file and a hint that names %s", line, cache.EnvDir)
		}
		// doctor and search agree.
		if err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{unModule}}); errs.Code(err) != errs.Cache {
			t.Errorf("Search() with the same cache: error = %v, want %s", err, errs.Cache)
		}
	})

	t.Run("below a read-only directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write everywhere")
		}
		w := newWorld(t)
		readOnly := filepath.Join(t.TempDir(), "read-only")
		if err := os.Mkdir(readOnly, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(readOnly, 0o755) })
		root := filepath.Join(readOnly, "cache")
		w.env[cache.EnvDir] = root

		err := w.a.Doctor(ctx, DoctorRequest{})
		if errs.Code(err) != DoctorFailed {
			t.Errorf("Doctor() error = %v, want %s", err, DoctorFailed)
		}
		line := doctorLine(t, w.stdout.String(), "cache")
		if !strings.HasPrefix(line, "cache: MISSING - E_CACHE: ") || !strings.Contains(line, readOnly+" is not writable") {
			t.Errorf("cache line = %q, want MISSING that names the directory that is not writable", line)
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Errorf("doctor created %s (stat error = %v), want it to create nothing", root, err)
		}
	})

	t.Run("a directory that does not exist yet is fine", func(t *testing.T) {
		w := newWorld(t)
		root := filepath.Join(t.TempDir(), "new", "cache")
		w.env[cache.EnvDir] = root
		if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
			t.Errorf("Doctor() error = %v, want nil\n%s", err, w.stdout.String())
		}
		if got, want := doctorLine(t, w.stdout.String(), "cache"), "cache: ok - "+root+" (0 platform dumps, 0 B)"; got != want {
			t.Errorf("cache line = %q, want %q", got, want)
		}
		if _, err := os.Stat(filepath.Dir(root)); !os.IsNotExist(err) {
			t.Errorf("doctor created %s, want it to create nothing", filepath.Dir(root))
		}
	})
}

// The java check is a run of a program, so it has a deadline like the rest.
func TestDoctorStopsAJavaThatHangs(t *testing.T) {
	w := newWorld(t)
	w.runner.Fakes["java"] = blocking
	w.a.limit = func(program string) time.Duration {
		if program == "java" {
			return 50 * time.Millisecond
		}
		return deadlineOf(program)
	}
	err := w.a.Doctor(context.Background(), DoctorRequest{})
	if errs.Code(err) != DoctorFailed {
		t.Errorf("Doctor() error = %v, want %s", err, DoctorFailed)
	}
	if line := doctorLine(t, w.stdout.String(), "java"); !strings.Contains(line, "java: MISSING - ") || !strings.Contains(line, "did not finish within 50ms") {
		t.Errorf("java line = %q, want MISSING that says java was stopped at its deadline", line)
	}
}
