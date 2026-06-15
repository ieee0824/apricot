package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersionInfo(t *testing.T) {
	tests := []struct {
		name          string
		ldVersion     string
		ldBuildTime   string
		info          *debug.BuildInfo
		ok            bool
		wantVersion   string
		wantBuildTime string
	}{
		{
			name:          "ldflags set both - used as-is",
			ldVersion:     "v1.2.3",
			ldBuildTime:   "2026-01-02T03:04:05Z",
			info:          nil,
			ok:            false,
			wantVersion:   "v1.2.3",
			wantBuildTime: "2026-01-02T03:04:05Z",
		},
		{
			name:          "no build info, empty version - dev",
			ok:            false,
			wantVersion:   "dev",
			wantBuildTime: "",
		},
		{
			name: "build info module version and vcs.time",
			ok:   true,
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.9.0"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abc"},
					{Key: "vcs.time", Value: "2026-06-15T00:00:00Z"},
				},
			},
			wantVersion:   "v0.9.0",
			wantBuildTime: "2026-06-15T00:00:00Z",
		},
		{
			name:          "build info devel version falls back to dev",
			ok:            true,
			info:          &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			wantVersion:   "dev",
			wantBuildTime: "",
		},
		{
			name:          "ldflags version kept, build time from vcs.time",
			ldVersion:     "v2.0.0",
			ok:            true,
			info:          &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.time", Value: "2026-05-01T00:00:00Z"}}},
			wantVersion:   "v2.0.0",
			wantBuildTime: "2026-05-01T00:00:00Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVersion, gotBuildTime := resolveVersionInfo(tt.ldVersion, tt.ldBuildTime, tt.info, tt.ok)
			if gotVersion != tt.wantVersion || gotBuildTime != tt.wantBuildTime {
				t.Errorf("resolveVersionInfo() = (%q, %q), want (%q, %q)",
					gotVersion, gotBuildTime, tt.wantVersion, tt.wantBuildTime)
			}
		})
	}
}
