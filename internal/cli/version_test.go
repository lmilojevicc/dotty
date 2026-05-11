package cli

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersionPrefersProvidedVersion(t *testing.T) {
	got := resolveVersion("v1.2.3", debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
	})
	if got != "v1.2.3" {
		t.Fatalf("version mismatch: want v1.2.3, got %s", got)
	}
}

func TestResolveVersionUsesBuildInfoModuleVersionWhenDev(t *testing.T) {
	got := resolveVersion("dev", debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
	})
	if got != "v1.2.3" {
		t.Fatalf("version mismatch: want v1.2.3, got %s", got)
	}
}

func TestResolveVersionLeavesDevWhenBuildInfoHasNoModuleVersion(t *testing.T) {
	got := resolveVersion("dev", debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
	})
	if got != "dev" {
		t.Fatalf("version mismatch: want dev, got %s", got)
	}
}
