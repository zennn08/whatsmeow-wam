package wam

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
)

//go:embed registry.json
var registryJSON []byte

type fieldDef struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
	Type string `json:"type"`
	Enum string `json:"enum,omitempty"`
}

type eventDef struct {
	ID      int        `json:"id"`
	Channel string     `json:"channel"`
	Weight  float64    `json:"weight"`
	Fields  []fieldDef `json:"fields"` // ordered: declaration order == wire order
}

type globalDef struct {
	ID       int      `json:"id"`
	Type     string   `json:"type"`
	Enum     string   `json:"enum,omitempty"`
	Channels []string `json:"channels"`
}

type wireFormat struct {
	Markers           map[string]int `json:"markers"`
	ValueEncodingBits map[string]int `json:"valueEncodingBits"`
}

type bufferConstants struct {
	MaxBufferSize                 int `json:"maxBufferSize"`
	InMemoryBufferingDurationSecs int `json:"inMemoryBufferingDurationSecs"`
}

type registry struct {
	ProtocolVersion  int                         `json:"protocolVersion"`
	ChannelWireCodes map[string]int              `json:"channelWireCodes"`
	BufferConstants  bufferConstants             `json:"bufferConstants"`
	WireFormat       wireFormat                  `json:"wireFormat"`
	Events           map[string]eventDef         `json:"events"`
	Enums            map[string]map[string]int64 `json:"enums"`
	Globals          map[string]globalDef        `json:"globals"`
}

var reg registry

func init() {
	if err := json.Unmarshal(registryJSON, &reg); err != nil {
		panic(fmt.Sprintf("wam: bad embedded registry: %v", err))
	}
}

// wamValueKind maps a registry field/global type to its wire encoding kind.
func wamValueKind(fieldType string) valueKind {
	switch fieldType {
	case "boolean":
		return kindBool
	case "integer", "timer", "enum":
		return kindInt
	case "number":
		return kindFloat
	case "string":
		return kindString
	default:
		return kindInvalid
	}
}

// UnresolvedEnumFields reports the payload keys that name an enum field of the
// event but carry a value that isn't a valid enum key (so the field would be
// silently dropped on commit). Empty slice means every enum field resolves. A
// test aid for validating fabricated payloads; returns a sentinel for an unknown
// event.
func UnresolvedEnumFields(event string, payload map[string]any) []string {
	def, ok := reg.Events[event]
	if !ok {
		return []string{"<unknown-event:" + event + ">"}
	}
	var bad []string
	for _, f := range def.Fields {
		raw, present := payload[f.Name]
		if !present || raw == nil || f.Type != "enum" {
			continue
		}
		if _, ok := resolveEnumValue(f.Enum, fmt.Sprint(raw)); !ok {
			bad = append(bad, f.Name)
		}
	}
	return bad
}

// resolveEnumValue resolves an enum value key (e.g. "CHAT_OPEN") to its numeric wire value.
func resolveEnumValue(enumName, key string) (int64, bool) {
	table, ok := reg.Enums[enumName]
	if !ok {
		return 0, false
	}
	v, ok := table[key]
	return v, ok
}

// resolveEventFields turns a typed event payload into the ordered wire-ready field
// list, converting enum keys to numeric ids and dropping absent, untyped, or
// unresolvable fields. Field order follows the registry's declared field order.
func resolveEventFields(def eventDef, payload map[string]any) []resolvedField {
	out := make([]resolvedField, 0, len(payload))
	for _, meta := range def.Fields {
		raw, present := payload[meta.Name]
		if !present || raw == nil {
			continue
		}
		kind := wamValueKind(meta.Type)
		if kind == kindInvalid {
			continue
		}
		switch {
		case meta.Type == "enum":
			numeric, ok := resolveEnumValue(meta.Enum, fmt.Sprint(raw))
			if !ok {
				continue
			}
			out = append(out, resolvedField{id: meta.ID, kind: kindInt, value: numeric})
		case kind == kindBool:
			out = append(out, resolvedField{id: meta.ID, kind: kind, value: toBool(raw)})
		case kind == kindString:
			out = append(out, resolvedField{id: meta.ID, kind: kind, value: fmt.Sprint(raw)})
		case kind == kindInt:
			n, ok := toInt64(raw)
			if !ok {
				continue
			}
			out = append(out, resolvedField{id: meta.ID, kind: kind, value: n})
		default: // kindFloat
			f, ok := toFloat64(raw)
			if !ok {
				continue
			}
			out = append(out, resolvedField{id: meta.ID, kind: kind, value: f})
		}
	}
	return out
}

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case int:
		return b != 0
	case int64:
		return b != 0
	case float64:
		return b != 0
	case string:
		return b != "" && b != "false" && b != "0"
	default:
		return v != nil
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return int64(n), true
	case bool:
		return boolToInt(n), true
	default:
		return 0, false
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
