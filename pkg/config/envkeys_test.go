package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGetHome_EnvOverride(t *testing.T) {
	t.Setenv(EnvHome, "/custom/reef")
	got := GetHome()
	if got != "/custom/reef" {
		t.Errorf("GetHome() = %q, want %q", got, "/custom/reef")
	}
}

func TestGetHome_DefaultFallback(t *testing.T) {
	t.Setenv(EnvHome, "")
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	t.Setenv("HOME", "/tmp/test-home")
	got := GetHome()
	want := filepath.Join("/tmp/test-home", ".reef")
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

	// Create .reef next to the real executable
	exePath, err := os.Executable()
	if err != nil {
		t.Skip("cannot get executable path")
	}
	exeDir := filepath.Dir(exePath)
	portableHome := filepath.Join(exeDir, ".reef")

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
	t.Setenv(EnvHome, "/custom/reef")
	got := GetOrCreateHome()
	if got != "/custom/reef" {
		t.Errorf("GetOrCreateHome() = %q, want %q", got, "/custom/reef")
	}
}

func TestGetOrCreateHome_FallbackToHome(t *testing.T) {
	t.Setenv(EnvHome, "")
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	t.Setenv("HOME", "/tmp/test-home")

	got := GetOrCreateHome()

	// GetOrCreateHome tries these locations in order:
	// 1. exe dir (.reef next to executable)
	// 2. cwd (.reef in current working directory, created if writable)
	// 3. ~/.reef
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	portableHome := filepath.Join(exeDir, ".reef")
	wd, _ := os.Getwd()
	cwdHome := filepath.Join(wd, ".reef")

	if got == portableHome {
		// Portable mode succeeded — .reef should exist next to exe
		if _, err := os.Stat(portableHome); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after GetOrCreateHome", portableHome)
		}
	} else if got == cwdHome {
		// CWD mode succeeded — .reef should exist in cwd
		if _, err := os.Stat(cwdHome); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after GetOrCreateHome", cwdHome)
		}
	} else if got == filepath.Join("/tmp/test-home", ".reef") {
		// Fell back to ~/.reef
		if _, err := os.Stat(got); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after GetOrCreateHome", got)
		}
	} else {
		t.Errorf("GetOrCreateHome() = %q, unexpected path", got)
	}
}

func TestGetOrCreateHome_EnvTakesPrecedence(t *testing.T) {
	t.Setenv(EnvHome, "/env/reef")
	t.Setenv("HOME", "/tmp/test-home")

	got := GetOrCreateHome()
	if got != "/env/reef" {
		t.Errorf("GetOrCreateHome() = %q, want %q", got, "/env/reef")
	}
}
