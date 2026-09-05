package blood

import (
	"regexp"
	"strings"
)

// Report is what the parser found in a document.
type Report struct {
	TakenOn   string
	Lab       string
	OrderedBy string
	Markers   []Marker
}

var (
	// Dynacare Plus: "   19         CHOLESTEROL, TOTAL        TEST STATUS"
	dynacareHeader = regexp.MustCompile(`^\s{2,}(\d{1,2})\s{4,}([A-Z][A-Z0-9 ,()/\-\.%*:]+?)\s{3,}TEST STATUS`)
	dynacareDate   = regexp.MustCompile(`DATE SAMPLES COLLECTED:\s+(\d{4}) (\w{3}) (\d{1,2})`)
	dynacareDoctor = regexp.MustCompile(`ORDERED BY:\s+(.+?)\s*$`)
	// Generic table row: "LDL Cholesterol   2.8   mmol/L   < 3.50"
	genericRow = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9 ,()/\-\.%]{2,60}?)\s{2,}(-?\d+(?:\.\d+)?)\s+([A-Za-z%/\*\d\.]+)?\s*(?:\s{2,}([<>]=?\s*\d+(?:\.\d+)?|\d+(?:\.\d+)?\s*-\s*\d+(?:\.\d+)?))?\s*([HL]|High|Low)?\s*$`)
	months     = map[string]string{"jan": "01", "feb": "02", "mar": "03", "apr": "04", "may": "05", "jun": "06", "jul": "07", "aug": "08", "sep": "09", "oct": "10", "nov": "11", "dec": "12"}
)

// Parse reads the text of a report. It recognises the Dynacare Plus layout
// in full, and falls back to reading any table-shaped lines it can find.
func Parse(text string) Report {
	lines := strings.Split(text, "\n")
	var rep Report
	if m := dynacareDate.FindStringSubmatch(text); m != nil {
		d := m[3]
		if len(d) == 1 {
			d = "0" + d
		}
		rep.TakenOn = m[1] + "-" + months[strings.ToLower(m[2])] + "-" + d
	}
	if strings.Contains(strings.ToLower(text), "dynacare") {
		rep.Lab = "Dynacare"
	}
	for _, l := range lines {
		if m := dynacareDoctor.FindStringSubmatch(l); m != nil {
			rep.OrderedBy = strings.TrimSpace(m[1])
			break
		}
	}

	seen := map[string]bool{}
	for i, l := range lines {
		m := dynacareHeader.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(m[2], ":"))
		// The header truncates long names ("ESTIMATED GLOMERULAR"); the
		// canonical table fixes the display name where it can.
		var mk Marker
		mk.Name = name
		// Read forward to the next test header, taking the first result and
		// the first range only: a later block belongs to a different test.
		for j := i + 1; j < len(lines) && j < i+40; j++ {
			if dynacareHeader.MatchString(lines[j]) {
				break
			}
			if idx := strings.Index(lines[j], "REFERENCE RANGE:"); idx >= 0 && mk.RefText == "" {
				mk.RefText = strings.TrimSpace(lines[j][idx+len("REFERENCE RANGE:"):])
			}
			if strings.Contains(lines[j], "YOUR RESULT") && j+1 < len(lines) && mk.ValueText == "" {
				mk.ValueText = strings.TrimSpace(lines[j+1])
			}
		}
		if mk.ValueText == "" {
			continue
		}
		key := m[1] + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		mk.Value, mk.Unit, mk.Flag = ParseResult(mk.ValueText)
		if mk.RefText != "" {
			var runit string
			mk.RefLow, mk.RefHigh, runit = ParseRange(mk.RefText)
			if mk.Unit == "" {
				mk.Unit = runit
			}
		}
		mk.Code = Canonical(mk.Name, mk.Unit)
		if d, ok := Lookup(mk.Code); ok {
			mk.Name = d.Name
		}
		if mk.Flag == "" {
			mk.Flag = Flag(mk.Value, mk.RefLow, mk.RefHigh)
		}
		rep.Markers = append(rep.Markers, mk)
	}
	if len(rep.Markers) > 0 {
		return rep
	}

	// Generic fallback: any line that reads like "name value unit range".
	for _, l := range lines {
		m := genericRow.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		code := Canonical(name, m[3])
		if code == "" {
			continue
		}
		if seen[code] {
			continue
		}
		seen[code] = true
		var mk Marker
		mk.Name = name
		mk.Value, mk.Unit, _ = ParseResult(m[2] + " " + m[3])
		mk.ValueText = strings.TrimSpace(m[2] + " " + m[3])
		mk.RefText = strings.TrimSpace(m[4])
		mk.RefLow, mk.RefHigh, _ = ParseRange(mk.RefText)
		mk.Code = code
		if d, ok := Lookup(code); ok {
			mk.Name = d.Name
		}
		switch strings.ToLower(m[5]) {
		case "h", "high":
			mk.Flag = "high"
		case "l", "low":
			mk.Flag = "low"
		default:
			mk.Flag = Flag(mk.Value, mk.RefLow, mk.RefHigh)
		}
		rep.Markers = append(rep.Markers, mk)
	}
	return rep
}
