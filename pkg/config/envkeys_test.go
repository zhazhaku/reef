package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGetHome_EnvOverride(t *testing.T) {
	t.Setenv(EnvHome, "/custom/picoclaw")
	got := GetHome()
	if got != "/custom/picoclaw" {
		t.Errorf("GetHome() = %q, want %q", got, "/custom/picoclaw")
	}
}

func TestGetHome_DefaultFallback(t *testing.T) {
	t.Setenv(EnvHome, "")
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	t.Setenv("HOME", "/tmp/test-home")
	got := GetHome()
	want := filepath.Join("/tmp/test-home", ".picoclaw")
	if got != want {
		t.Errorf("GetHome() = %q, want %q", got, want)
	}
}

func TestGetHome_PortableMode_ExistingDir(t *testing.T) {
	t.Setenv(EnvHome, "")
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	t.Setenv("HOME", "/tmp/test-home")

	// Create .picoclaw next to the real executable
	exePath, err := os.Executable()
	if err != nil {
		t.Skip("cannot get executable path")
	}
	exeDir := filepath.Dir(exePath)
	portableHome := filepath.Join(exeDir, ".picoclaw")

	// Create it temporarily
	if err := os.MkdirAll(portableHome, 0o755); err != nil {
		t.Skip("cannot create portable home (read-only exe dir?)")
	}
	defer os.RemoveAll(portableHome)

	got := GetHome()
	if got != portableHome {
		t.Errorf("GetHome() = %q, want %q (portable mode)", got, portableHome)
	}
}

func TestGetOrCreateHome_EnvOverride(t *testing.T) {
	t.Setenv(EnvHome, "/custom/picoclaw")
	got := GetOrCreateHome()
	if got != "/custom/picoclaw" {
		t.Errorf("GetOrCreateHome() = %q, want %q", got, "/custom/picoclaw")
	}
}

func TestGetOrCreateHome_FallbackToHome(t *testing.T) {
	t.Setenv(EnvHome, "")
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	t.Setenv("HOME", "/tmp/test-home")

	got := GetOrCreateHome()

	// GetOrCreateHome tries exe dir first. If the exe dir is writable
	// (e.g., /tmp/go-build.../), it will create .picoclaw there.
	// If not, it falls back to ~/.picoclaw.
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	portableHome := filepath.Join(exeDir, ".picoclaw")

	if got == portableHome {
		// Portable mode succeeded — .picoclaw should exist next to exe
		if _, err := os.Stat(portableHome); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after GetOrCreateHome", portableHome)
		}
	} else if got == filepath.Join("/tmp/test-home", ".picoclaw") {
		// Fell back to ~/.picoclaw
		if _, err := os.Stat(got); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after GetOrCreateHome", got)
		}
	} else {
		t.Errorf("GetOrCreateHome() = %q, unexpected path", got)
	}
}

func TestGetOrCreateHome_EnvTakesPrecedence(t *testing.T) {
	t.Setenv(EnvHome, "/env/picoclaw")
	t.Setenv("HOME", "/tmp/test-home")

	got := GetOrCreateHome()
	if got != "/env/picoclaw" {
		t.Errorf("GetOrCreateHome() = %q, want %q", got, "/env/picoclaw")
	}
}
