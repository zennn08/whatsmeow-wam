package wam

import (
	"slices"
	"strings"
)

// GlobalsInput are the batch-level global inputs a headless client can honestly
// populate. Resolve platform/browser/OS from the same identity the client
// advertises in pairing so the globals never contradict the ClientPayload — an
// inconsistent global is a worse fingerprint than none.
type GlobalsInput struct {
	// OSDisplayName drives webcWebPlatform (WIN32/DARWIN/WEB) and osVersion, e.g. "Windows 10".
	OSDisplayName string
	// Browser display name, e.g. "Chrome".
	Browser string
	// AppVersion advertised, e.g. "2.3000.1234567890".
	AppVersion string
	// StreamID stamped both in the header and as the streamId global.
	StreamID int
	// ServiceImprovementOptOut consent bit.
	ServiceImprovementOptOut bool
}

// namedGlobal is one global attribute keyed by its registry name (enum values as
// their key string, converted to numeric during resolution).
type namedGlobal struct {
	name  string
	value any
}

// webcWebPlatformKey maps an OS display name to the WEBC_WEB_PLATFORM_TYPE enum key.
func webcWebPlatformKey(osDisplayName string) string {
	os := strings.ToLower(osDisplayName)
	switch {
	case strings.Contains(os, "win"):
		return "WIN32"
	case strings.Contains(os, "mac"), strings.Contains(os, "os x"), strings.Contains(os, "darwin"):
		return "DARWIN"
	default:
		return "WEB"
	}
}

// buildNamedGlobals is the globals a headless client can honestly populate, in the
// fixed order WA Web writes them (order is part of the wire fingerprint).
func buildNamedGlobals(in GlobalsInput) []namedGlobal {
	return []namedGlobal{
		{"platform", "WEBCLIENT"},
		{"webcWebPlatform", webcWebPlatformKey(in.OSDisplayName)},
		{"appVersion", in.AppVersion},
		{"osVersion", in.OSDisplayName},
		{"browser", in.Browser},
		{"deviceName", "Desktop"},
		{"streamId", in.StreamID},
		{"mcc", 0},
		{"mnc", 0},
		{"serviceImprovementOptOut", in.ServiceImprovementOptOut},
	}
}

// resolveGlobals resolves the named globals into the ordered id-keyed list a batch
// consumes for a given channel: filters to globals valid on that channel and
// converts enum globals from their key string to the numeric value.
func resolveGlobals(in GlobalsInput, channel string) []globalKV {
	out := make([]globalKV, 0, 10)
	for _, ng := range buildNamedGlobals(in) {
		if ng.value == nil {
			continue
		}
		g, ok := reg.Globals[ng.name]
		if !ok {
			continue
		}
		if !channelAllowed(g.Channels, channel) {
			continue
		}
		if g.Type == "enum" {
			numeric, ok := resolveEnumValue(g.Enum, toStr(ng.value))
			if !ok {
				continue
			}
			out = append(out, globalKV{id: g.ID, value: numeric})
		} else {
			out = append(out, globalKV{id: g.ID, value: ng.value})
		}
	}
	return out
}

func channelAllowed(channels []string, channel string) bool {
	return slices.Contains(channels, channel)
}

func toStr(v any) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		return ""
	}
}
