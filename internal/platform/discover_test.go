package platform

import (
	"strings"
	"testing"
)

// currentVersionRb mirrors Homebrew/brew's macos_version.rb after
// "Decouple macOS release names from support": every named release lives in
// RELEASES and SYMBOLS is the supported subset.
const currentVersionRb = `
class MacOSVersion < Version
  # Retain named releases after support ends so historical data can still be labelled.
  RELEASES = T.let({
    golden_gate: "27",
    tahoe:       "26",
    sequoia:     "15",
    sonoma:      "14",
    ventura:     "13",
    monterey:    "12",
    # odisabled: remove support for Big Sur and macOS x86_64 September (or later) 2027
    big_sur:     "11",
    catalina:    "10.15",
    mojave:      "10.14",
    high_sierra: "10.13",
    sierra:      "10.12",
    el_capitan:  "10.11",
  }.freeze, T::Hash[Symbol, String])

  # Big Sur reports 10.16 under SYSTEM_VERSION_COMPAT.
  RELEASE_ALIASES = T.let({
    "10.16" => "11",
  }.freeze, T::Hash[String, String])

  # NOTE: When removing support, exclude the symbol here and add it to
  #       ` + "`DISABLED_MACOS_VERSIONS`" + ` in ` + "`MacOSRequirement`" + `.
  SYMBOLS = T.let(RELEASES.except(
    :catalina,
    :mojave,
    :high_sierra,
    :sierra,
    :el_capitan,
  ).freeze, T::Hash[Symbol, String])
end
`

// legacyVersionRb is the older literal layout.
const legacyVersionRb = `
class MacOSVersion < Version
  SYMBOLS = T.let({
    tahoe:    "26",
    sequoia:  "15",
    sonoma:   "14",
    ventura:  "13",
    monterey: "12",
    big_sur:  "11",
    catalina: "10.15",
  }.freeze, T::Hash[Symbol, String])
end
`

func symbols(info *PlatformInfo) string {
	var out []string
	for _, v := range info.MacOSVersions {
		out = append(out, v.Symbol)
	}
	return strings.Join(out, ",")
}

func TestParseVersionRbCurrentLayout(t *testing.T) {
	info, err := parseVersionRb(currentVersionRb)
	if err != nil {
		t.Fatalf("parseVersionRb: %v", err)
	}
	// Newest first; big_sur and older fall below MinSupportedMacOSMajor, and
	// the RELEASES.except list never leaks in.
	if got, want := symbols(info), "golden_gate,tahoe,sequoia,sonoma,ventura,monterey"; got != want {
		t.Errorf("versions = %s, want %s", got, want)
	}
	if info.MacOSVersions[len(info.MacOSVersions)-1].Major != 12 {
		t.Errorf("oldest = %+v, want monterey/12", info.MacOSVersions[len(info.MacOSVersions)-1])
	}
}

func TestParseVersionRbLegacyLayout(t *testing.T) {
	info, err := parseVersionRb(legacyVersionRb)
	if err != nil {
		t.Fatalf("parseVersionRb: %v", err)
	}
	if got, want := symbols(info), "tahoe,sequoia,sonoma,ventura,monterey"; got != want {
		t.Errorf("versions = %s, want %s", got, want)
	}
}

func TestParseVersionRbExceptHonoursExclusions(t *testing.T) {
	// If brew ever drops monterey via except, it must disappear even though
	// it clears the minimum major.
	content := strings.Replace(currentVersionRb, ":catalina,", ":monterey,\n    :catalina,", 1)
	info, err := parseVersionRb(content)
	if err != nil {
		t.Fatalf("parseVersionRb: %v", err)
	}
	if strings.Contains(symbols(info), "monterey") {
		t.Errorf("excluded symbol survived: %s", symbols(info))
	}
}

func TestParseVersionRbMissing(t *testing.T) {
	if _, err := parseVersionRb("class MacOSVersion; end"); err == nil {
		t.Error("expected an error without a SYMBOLS hash")
	}
	if _, err := parseVersionRb("SYMBOLS = T.let(RELEASES.except(:x).freeze, T::Hash[Symbol, String])"); err == nil {
		t.Error("expected an error when RELEASES is missing")
	}
}
