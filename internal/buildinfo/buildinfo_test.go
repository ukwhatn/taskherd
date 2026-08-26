package buildinfo

import "testing"

// The distinction this makes is what stops the updater from overwriting a binary someone built
// themselves: a plain `go build` inside a checkout records a pseudo-version, not "(devel)", and a
// pseudo-version reads like a release until it is recognised for what it is.
func TestModuleVersion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		main  string
		dirty bool
		want  string
	}{
		{"go install したタグ", "v1.2.3", false, "1.2.3"},
		{"prerelease タグ", "v1.2.3-rc1", false, "1.2.3-rc1"},
		{"モジュール外のビルド", "(devel)", false, DevVersion},
		{"記録が無い", "", false, DevVersion},
		{"擬似バージョン", "v0.0.0-20260826115506-827dbefc79e6", false, DevVersion},
		{"タグ付きコミットの後の擬似バージョン", "v1.2.4-0.20260826115506-827dbefc79e6", false, DevVersion},
		{"タグはあるが作業ツリーが汚れている", "v1.2.3", true, DevVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := moduleVersion(tc.main, tc.dirty); got != tc.want {
				t.Errorf("moduleVersion(%q, %v) = %q, want %q", tc.main, tc.dirty, got, tc.want)
			}
		})
	}
}

func TestIsPseudoVersionLeavesRealTagsAlone(t *testing.T) {
	for _, v := range []string{
		"1.2.3",
		"1.2.3-rc1",
		"1.2.3-20260826115506",              // timestamp but no revision
		"1.2.3-827dbefc79e6",                // revision but no timestamp
		"1.2.3-2026082611550-827dbefc79e6",  // 13-digit timestamp
		"1.2.3-20260826115506-827dbefc79e",  // 11-character revision
		"1.2.3-20260826115506-827dbefc79eZ", // not hex
	} {
		if isPseudoVersion(v) {
			t.Errorf("isPseudoVersion(%q) = true, want false", v)
		}
	}
}

func TestReleased(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{"1.2.3", true},
		{DevVersion, false},
		{"", false},
	} {
		if got := (Info{Version: tc.version}).Released(); got != tc.want {
			t.Errorf("Info{%q}.Released() = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestInfoString(t *testing.T) {
	for _, tc := range []struct {
		name string
		info Info
		want string
	}{
		{"全部そろっている", Info{Version: "1.2.3", Commit: "827dbefc79e6ac6", Date: "2026-08-26"}, "1.2.3 (827dbef, 2026-08-26)"},
		{"commit だけ", Info{Version: "1.2.3", Commit: "827dbef"}, "1.2.3 (827dbef)"},
		{"date だけ", Info{Version: "1.2.3", Date: "2026-08-26"}, "1.2.3 (2026-08-26)"},
		{"version だけ", Info{Version: DevVersion}, DevVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The linker flags have to win: a released binary knows its own tag, and nothing reconstructed
// from the toolchain should be able to contradict it.
func TestStampedValuesWin(t *testing.T) {
	Version, Commit, Date = "v9.9.9", "deadbeefcafe", "2026-01-01T00:00:00Z"
	t.Cleanup(func() { Version, Commit, Date = "", "", "" })

	got := Get()
	if got.Version != "9.9.9" || got.Commit != "deadbeefcafe" || got.Date != "2026-01-01T00:00:00Z" {
		t.Errorf("Get() = %+v, want ldflags の値", got)
	}
	if !got.Released() {
		t.Error("Released() = false, want true")
	}
}

// A test binary is built from the working tree, so this is the same path a `go build` takes.
func TestGetWithoutStampsReportsDev(t *testing.T) {
	got := Get()
	if got.Version != DevVersion {
		t.Errorf("Version = %q, want %q", got.Version, DevVersion)
	}
	if got.Go == "" || got.OS == "" || got.Arch == "" {
		t.Errorf("ツールチェーン情報が埋まっていない: %+v", got)
	}
}
