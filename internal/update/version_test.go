package update

import "testing"

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		name      string
		current   string
		candidate string
		want      bool
	}{
		{"patch が上がった", "1.2.3", "v1.2.4", true},
		{"minor が上がった", "1.2.3", "v1.3.0", true},
		{"major が上がった", "1.2.3", "v2.0.0", true},
		{"同じ", "1.2.3", "v1.2.3", false},
		{"古い", "1.2.4", "v1.2.3", false},
		{"minor は上がったが major が下がった", "2.0.0", "v1.9.9", false},
		{"v の有無を吸収する", "v1.2.3", "1.2.4", true},
		{"ビルドメタデータは順序に効かない", "1.2.3+abc", "v1.2.3+def", false},

		// A prerelease is never offered: /releases/latest does not serve them, so a candidate
		// carrying one arrived by some other route and should not be installed unasked.
		{"候補が prerelease なら新しくない", "1.2.3", "v1.3.0-rc1", false},
		// The finished release does supersede the prerelease that led to it.
		{"prerelease から同番の正式版へは上がる", "1.3.0-rc1", "v1.3.0", true},
		{"prerelease から次の版へも上がる", "1.3.0-rc1", "v1.3.1", true},

		// dev is what an unreleased build reports; nothing is newer than something unparsable,
		// because "update" would mean overwriting a binary with no counterpart on the releases page.
		{"開発ビルドには何も勧めない", "dev", "v1.2.3", false},
		{"現在が空", "", "v1.2.3", false},
		{"候補が空", "1.2.3", "", false},
		{"候補が数字でない", "1.2.3", "latest", false},
		{"要素が 3 つない", "1.2.3", "v1.3", false},
		{"負の数", "1.2.3", "v1.-1.0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Newer(tc.current, tc.candidate); got != tc.want {
				t.Errorf("Newer(%q, %q) = %v, want %v", tc.current, tc.candidate, got, tc.want)
			}
		})
	}
}
