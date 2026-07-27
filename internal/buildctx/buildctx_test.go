package buildctx

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFiles creates a context directory from a map of relative path → content.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func mustPrepare(t *testing.T, ctx, dockerfile string) string {
	t.Helper()
	dir, cleanup, err := Prepare(ctx, dockerfile)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if dir == "" {
		t.Fatal("Prepare skipped filtering, want a filtered copy")
	}
	t.Cleanup(cleanup)
	return dir
}

func assertExists(t *testing.T, dir, rel string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, rel)); err != nil {
		t.Errorf("%s: should exist in filtered context: %v", rel, err)
	}
}

func assertNotExists(t *testing.T, dir, rel string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, rel)); err == nil {
		t.Errorf("%s: should be excluded from filtered context", rel)
	}
}

func TestPrepare_FiltersIgnoredFiles(t *testing.T) {
	ctx := writeFiles(t, map[string]string{
		"Dockerfile":       "FROM alpine",
		".dockerignore":    "target/\n*.log\n",
		"src/main.rs":      "fn main() {}",
		"target/debug/bin": "binary",
		"build.log":        "log",
	})

	dir := mustPrepare(t, ctx, "")
	assertExists(t, dir, "Dockerfile")
	assertExists(t, dir, ".dockerignore")
	assertExists(t, dir, "src/main.rs")
	assertNotExists(t, dir, "target")
	assertNotExists(t, dir, "build.log")
}

func TestPrepare_NegationReincludes(t *testing.T) {
	ctx := writeFiles(t, map[string]string{
		"Dockerfile":    "FROM alpine",
		".dockerignore": "junk/\n!junk/keep.txt\n",
		"junk/keep.txt": "keep",
		"junk/drop.txt": "drop",
	})

	dir := mustPrepare(t, ctx, "")
	assertExists(t, dir, "junk/keep.txt")
	assertNotExists(t, dir, "junk/drop.txt")
}

func TestPrepare_ForceIncludesDockerfileAndIgnoreFile(t *testing.T) {
	// Patterns that match the Dockerfile and .dockerignore themselves must
	// not exclude them (same rule as docker build).
	ctx := writeFiles(t, map[string]string{
		"docker/app.Dockerfile": "FROM alpine",
		".dockerignore":         "*\n",
		"secret.txt":            "x",
	})

	dir := mustPrepare(t, ctx, "docker/app.Dockerfile")
	assertExists(t, dir, "docker/app.Dockerfile")
	assertExists(t, dir, ".dockerignore")
	assertNotExists(t, dir, "secret.txt")
}

func TestPrepare_CopiesSymlinksAsSymlinks(t *testing.T) {
	ctx := writeFiles(t, map[string]string{
		"Dockerfile":    "FROM alpine",
		".dockerignore": "ignored/\n",
		"real.txt":      "content",
	})
	if err := os.Symlink("real.txt", filepath.Join(ctx, "link.txt")); err != nil {
		t.Fatal(err)
	}

	dir := mustPrepare(t, ctx, "")
	target, err := os.Readlink(filepath.Join(dir, "link.txt"))
	if err != nil {
		t.Fatalf("link.txt should be a symlink: %v", err)
	}
	if target != "real.txt" {
		t.Errorf("symlink target = %q, want %q", target, "real.txt")
	}
}

func TestPrepare_SkipsWhenNoDockerignore(t *testing.T) {
	ctx := writeFiles(t, map[string]string{"Dockerfile": "FROM alpine"})
	dir, cleanup, err := Prepare(ctx, "")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if dir != "" || cleanup != nil {
		t.Errorf("Prepare = (%q, cleanup nil=%v), want skip without .dockerignore", dir, cleanup == nil)
	}
}

func TestPrepare_SkipsForAbsoluteDockerfile(t *testing.T) {
	ctx := writeFiles(t, map[string]string{
		".dockerignore": "junk/\n",
	})
	dir, _, err := Prepare(ctx, "/somewhere/else/Dockerfile")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if dir != "" {
		t.Error("Prepare should skip filtering for an out-of-context Dockerfile")
	}
}

func TestPrepare_SkipsWhenDisabledByEnv(t *testing.T) {
	ctx := writeFiles(t, map[string]string{
		"Dockerfile":    "FROM alpine",
		".dockerignore": "junk/\n",
	})
	t.Setenv(DisableEnv, "1")
	dir, _, err := Prepare(ctx, "")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if dir != "" {
		t.Errorf("Prepare = %q, want skip when %s is set", dir, DisableEnv)
	}
}

func TestPrepare_CleanupRemovesCopy(t *testing.T) {
	ctx := writeFiles(t, map[string]string{
		"Dockerfile":    "FROM alpine",
		".dockerignore": "junk/\n",
		"a.txt":         "a",
	})
	dir, cleanup, err := Prepare(ctx, "")
	if err != nil || dir == "" {
		t.Fatalf("Prepare = (%q, %v)", dir, err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cleanup did not remove %s", dir)
	}
}

func TestPrepare_PreservesFileContent(t *testing.T) {
	ctx := writeFiles(t, map[string]string{
		"Dockerfile":    "FROM alpine",
		".dockerignore": "junk/\n",
		"src/app.go":    "package main",
	})

	dir := mustPrepare(t, ctx, "")
	got, err := os.ReadFile(filepath.Join(dir, "src/app.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main" {
		t.Errorf("content = %q, want %q", got, "package main")
	}
}
