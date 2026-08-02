package system

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

func TestIsRunningInContainer(t *testing.T) {
	t.Parallel()

	t.Run("dockerenv file present", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if _, err := fs.Create("/.dockerenv"); err != nil {
			t.Fatalf("create /.dockerenv: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when /.dockerenv exists")
		}
	})

	t.Run("containerenv file present (podman/nerdctl)", func(t *testing.T) {
		t.Parallel()
		// Podman and nerdctl create /run/.containerenv instead of /.dockerenv.
		// Without this marker a podman container would fall through to the
		// cgroup substring match, which on cgroup v2 reads just `0::/` and
		// misses — misclassifying the container as CLI and attempting a
		// self-swap the image would lose on recreate.
		fs := afero.NewMemMapFs()
		if _, err := fs.Create("/run/.containerenv"); err != nil {
			t.Fatalf("create /run/.containerenv: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when /run/.containerenv exists")
		}
	})

	t.Run("cgroup mentions docker", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "/proc/1/cgroup", []byte("0::/docker/abcdef123456"), 0o644); err != nil {
			t.Fatalf("write cgroup: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when cgroup mentions docker")
		}
	})

	t.Run("cgroup mentions containerd", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "/proc/1/cgroup", []byte("0::/system.slice/containerd.service"), 0o644); err != nil {
			t.Fatalf("write cgroup: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when cgroup mentions containerd")
		}
	})

	t.Run("cgroup mentions kubepods", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "/proc/1/cgroup", []byte("12:cpuset:/kubepods/podabc"), 0o644); err != nil {
			t.Fatalf("write cgroup: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when cgroup mentions kubepods")
		}
	})

	t.Run("bare host: no dockerenv, no cgroup", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if IsRunningInContainer(fs) {
			t.Fatal("expected no container detection on a bare host")
		}
	})

	t.Run("cgroup without container markers", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "/proc/1/cgroup", []byte("0::/system.slice/sshd.service"), 0o644); err != nil {
			t.Fatalf("write cgroup: %v", err)
		}
		if IsRunningInContainer(fs) {
			t.Fatal("expected no container detection for an ordinary cgroup")
		}
	})
}

func TestDetectEnvironment(t *testing.T) {
	t.Parallel()

	t.Run("desktop short-circuits before docker probe", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if _, err := fs.Create("/.dockerenv"); err != nil {
			t.Fatalf("create /.dockerenv: %v", err)
		}
		got := DetectEnvironment(fs, true)
		if got != EnvironmentDesktop {
			t.Fatalf("DetectEnvironment(isDesktop=true) = %q, want %q", got, EnvironmentDesktop)
		}
	})

	t.Run("cli build in container is docker", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if _, err := fs.Create("/.dockerenv"); err != nil {
			t.Fatalf("create /.dockerenv: %v", err)
		}
		got := DetectEnvironment(fs, false)
		if got != EnvironmentDocker {
			t.Fatalf("DetectEnvironment(isDesktop=false, in container) = %q, want %q", got, EnvironmentDocker)
		}
	})

	t.Run("cli build on bare host is cli", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		got := DetectEnvironment(fs, false)
		if got != EnvironmentCLI {
			t.Fatalf("DetectEnvironment(isDesktop=false, bare host) = %q, want %q", got, EnvironmentCLI)
		}
	})
}

func TestUpgradeInstructions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  Environment
		want string
	}{
		{"docker mentions docker pull", EnvironmentDocker, "docker pull"},
		{"docker mentions image ref", EnvironmentDocker, "ghcr.io/javinizer/javinizer-go"},
		{"desktop mentions releases", EnvironmentDesktop, "releases"},
		{"cli mentions javinizer upgrade", EnvironmentCLI, "javinizer upgrade"},
		{"cli mentions brew fallback", EnvironmentCLI, "brew upgrade javinizer"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := UpgradeInstructions(tc.env)
			if !contains(got, tc.want) {
				t.Fatalf("UpgradeInstructions(%q) = %q, want substring %q", tc.env, got, tc.want)
			}
		})
	}
}

func TestUpgradeCommands(t *testing.T) {
	t.Parallel()

	t.Run("docker returns pull + compose commands with the published image ref", func(t *testing.T) {
		t.Parallel()
		got := UpgradeCommands(EnvironmentDocker, "linux", false, "", "manual")
		if len(got) != 2 {
			t.Fatalf("UpgradeCommands(docker) returned %d rows, want 2", len(got))
		}
		if got[0].Key != "docker_pull" || !contains(got[0].Command, dockerImageRef) {
			t.Errorf("row 0 = %+v, want docker_pull containing %q", got[0], dockerImageRef)
		}
		if got[1].Key != "docker_compose" || got[1].Command != "docker compose pull && docker compose up -d" {
			t.Errorf("row 1 = %+v, want the compose pull+up command", got[1])
		}
	})

	t.Run("desktop returns no commands (in-app upgrade only)", func(t *testing.T) {
		t.Parallel()
		if got := UpgradeCommands(EnvironmentDesktop, "darwin", false, "", "manual"); got != nil {
			t.Fatalf("UpgradeCommands(desktop) = %+v, want nil", got)
		}
	})

	// The package-manager row is host-OS-specific: Homebrew is macOS/Linux
	// (Cellar path detection, see internal/update/upgrade.go), Scoop is
	// Windows-only — no OS ever gets both rows.
	for _, tc := range []struct {
		name     string
		goos     string
		wantKeys []string
	}{
		{"darwin offers homebrew", "darwin", []string{"cli_binary", "homebrew"}},
		{"linux offers homebrew", "linux", []string{"cli_binary", "homebrew"}},
		{"windows offers scoop", "windows", []string{"cli_binary", "scoop"}},
		{"other OSes get the universal row only", "freebsd", []string{"cli_binary"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := UpgradeCommands(EnvironmentCLI, tc.goos, false, "", "manual")
			gotKeys := make([]string, 0, len(got))
			for _, cmd := range got {
				gotKeys = append(gotKeys, cmd.Key)
			}
			if !reflect.DeepEqual(gotKeys, tc.wantKeys) {
				t.Fatalf("UpgradeCommands(cli, %q) keys = %v, want %v", tc.goos, gotKeys, tc.wantKeys)
			}
		})
	}

	t.Run("prerelease announcements swap the CLI row and drop package managers", func(t *testing.T) {
		t.Parallel()
		// `javinizer upgrade` is stable-only by default, so announcing a
		// prerelease must offer the opt-in flag; Homebrew/Scoop publish only
		// stable builds, so their rows vanish regardless of host OS.
		for _, goos := range []string{"darwin", "linux", "windows"} {
			got := UpgradeCommands(EnvironmentCLI, goos, true, "v9.9.9-rc1", "manual")
			if len(got) != 1 || got[0].Key != "cli_binary" || got[0].Command != "javinizer upgrade --prerelease" {
				t.Errorf("UpgradeCommands(cli, %q, prerelease=true) = %+v, want single --prerelease CLI row", goos, got)
			}
		}
	})

	t.Run("prerelease on package-managed installs offers no broken command", func(t *testing.T) {
		t.Parallel()
		// `javinizer upgrade` hands brew/scoop installs off to the package
		// manager, which publishes stable only — the announced prerelease is
		// unreachable; nil (no rows) beats a nonfunctional command.
		for _, method := range []string{"homebrew", "scoop"} {
			if got := UpgradeCommands(EnvironmentCLI, "darwin", true, "v9.9.9-rc1", method); got != nil {
				t.Errorf("UpgradeCommands(cli, prerelease=true, %s) = %+v, want nil", method, got)
			}
		}
		// Unknown/manual installs still get the working self-upgrade.
		if got := UpgradeCommands(EnvironmentCLI, "darwin", true, "v9.9.9-rc1", "manual"); len(got) != 1 {
			t.Errorf("UpgradeCommands(cli, prerelease=true, manual) returned %d rows, want 1", len(got))
		}
	})

	t.Run("docker prerelease pins the announced tag and drops the compose row", func(t *testing.T) {
		t.Parallel()
		// `:latest` is stable-only — a prerelease announcement must point at
		// its exact tag, and the compose row (which re-pulls whatever the
		// compose file pins) cannot express that.
		got := UpgradeCommands(EnvironmentDocker, "linux", true, "v9.9.9-rc1", "manual")
		if len(got) != 1 {
			t.Fatalf("UpgradeCommands(docker, prerelease=true) returned %d rows, want 1", len(got))
		}
		want := "docker pull " + dockerImageRef + ":v9.9.9-rc1"
		if got[0].Key != "docker_pull" || got[0].Command != want {
			t.Errorf("row 0 = %+v, want docker_pull %q", got[0], want)
		}
	})

	t.Run("docker stable pins the announced tag when known", func(t *testing.T) {
		t.Parallel()
		got := UpgradeCommands(EnvironmentDocker, "linux", false, "v1.4.0", "manual")
		if len(got) != 2 {
			t.Fatalf("UpgradeCommands(docker) returned %d rows, want 2", len(got))
		}
		if got[0].Command != "docker pull "+dockerImageRef+":v1.4.0" {
			t.Errorf("row 0 = %+v, want pull pinned to :v1.4.0", got[0])
		}
	})

	t.Run("every command is prose-free (single pasteable line)", func(t *testing.T) {
		t.Parallel()
		for _, env := range []Environment{EnvironmentCLI, EnvironmentDocker} {
			for _, cmd := range UpgradeCommands(env, runtime.GOOS, false, "", "manual") {
				if cmd.Key == "" || cmd.Command == "" {
					t.Errorf("env=%q row %+v has an empty key or command", env, cmd)
				}
				if strings.Contains(cmd.Command, "\n") {
					t.Errorf("env=%q command %q spans multiple lines", env, cmd.Command)
				}
			}
		}
	})
}
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
