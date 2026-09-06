// Package blood parses lab reports and names markers consistently.
//
// A lab calls the same test different things ("ALANINE AMINOTRANSFERASE",
// "ALT (SGPT)"); the canonical code is what lets two reports line up on one
// chart. The parser is deliberately tolerant: a report is a source document,
// and anything it cannot read is kept as text rather than dropped.
package blood

import (
	"regexp"
	"strconv"
	"strings"
)

// Marker is one measured value.
type Marker struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Value     *float64 `json:"value"`
	ValueText string   `json:"value_text"`
	Unit      string   `json:"unit"`
	RefLow    *float64 `json:"ref_low"`
	RefHigh   *float64 `json:"ref_high"`
	RefText   string   `json:"ref_text"`
	Flag      string   `json:"flag"`
}

// Definition describes a canonical marker.
type Definition struct {
	Code     string
	Name     string
	Category string
	// LowerIsBetter says which direction counts as improvement, for trends.
	LowerIsBetter bool
	// Watch marks the markers the summary leads with.
	Watch bool
	// Aliases are lower-case substrings that identify the test.
	Aliases []string
}

// Definitions is the canonical marker table, in display order.
var Definitions = []Definition{
	{"hba1c", "HbA1c", "sugar", true, true, []string{"a1c"}},
	{"glucose_fasting", "Fasting glucose", "sugar", true, true, []string{"glucose, fasting", "fasting glucose", "glucose fasting"}},
	{"glucose_random", "Glucose", "sugar", true, false, []string{"glucose, random", "random glucose", "glucose"}},
	{"insulin", "Insulin", "sugar", true, false, []string{"insulin"}},
	{"total_cholesterol", "Total cholesterol", "lipids", true, true, []string{"cholesterol, total", "total cholesterol"}},
	{"ldl", "LDL cholesterol", "lipids", true, true, []string{"ldl"}},
	{"hdl", "HDL cholesterol", "lipids", false, true, []string{"hdl cholesterol", "hdl-c", "hdl"}},
	{"non_hdl", "Non-HDL cholesterol", "lipids", true, true, []string{"non-hdl", "non hdl"}},
	{"triglycerides", "Triglycerides", "lipids", true, true, []string{"triglyceride"}},
	{"chol_hdl_ratio", "Cholesterol/HDL ratio", "lipids", true, false, []string{"chol/hdl", "cholesterol/hdl", "hdl ratio"}},
	{"apob", "Apolipoprotein B", "lipids", true, false, []string{"apolipoprotein b", "apob"}},
	{"lpa", "Lipoprotein(a)", "lipids", true, false, []string{"lipoprotein(a)", "lp(a)"}},
	{"alt", "ALT", "liver", true, true, []string{"alanine", "alt", "sgpt"}},
	{"ast", "AST", "liver", true, false, []string{"aspartate", "ast", "sgot"}},
	{"alp", "ALP", "liver", true, false, []string{"alkaline phosphatase", "alp"}},
	{"ggt", "GGT", "liver", true, false, []string{"gamma", "ggt"}},
	{"bilirubin", "Bilirubin, total", "liver", true, false, []string{"bilirubin"}},
	{"albumin", "Albumin", "liver", false, false, []string{"albumin"}},
	{"egfr", "eGFR", "kidney", false, false, []string{"glomerular", "egfr"}},
	{"creatinine", "Creatinine", "kidney", true, false, []string{"creatinine"}},
	{"urea", "Urea", "kidney", true, false, []string{"urea", "bun"}},
	{"acr", "Albumin/creatinine ratio", "kidney", true, false, []string{"albumin / creatinine", "albumin/creatinine", "acr"}},
	{"uric_acid", "Uric acid", "kidney", true, false, []string{"uric", "urate"}},
	{"sodium", "Sodium", "electrolytes", false, false, []string{"sodium"}},
	{"potassium", "Potassium", "electrolytes", false, false, []string{"potassium"}},
	{"chloride", "Chloride", "electrolytes", false, false, []string{"chloride"}},
	{"calcium", "Calcium", "electrolytes", false, false, []string{"calcium"}},
	{"magnesium", "Magnesium", "electrolytes", false, false, []string{"magnesium"}},
	{"tsh", "TSH", "thyroid", false, false, []string{"thyroid stimulating", "tsh"}},
	{"free_t4", "Free T4", "thyroid", false, false, []string{"free t4", "thyroxine"}},
	{"free_t3", "Free T3", "thyroid", false, false, []string{"free t3"}},
	{"testosterone", "Testosterone, total", "hormones", false, false, []string{"testosterone"}},
	{"cortisol", "Cortisol", "hormones", false, false, []string{"cortisol"}},
	{"vitamin_d", "Vitamin D", "vitamins", false, false, []string{"vitamin d", "25-hydroxy"}},
	{"b12", "Vitamin B12", "vitamins", false, false, []string{"b12", "cobalamin"}},
	{"folate", "Folate", "vitamins", false, false, []string{"folate"}},
	{"ferritin", "Ferritin", "iron", false, false, []string{"ferritin"}},
	{"iron", "Iron", "iron", false, false, []string{"iron"}},
	{"crp", "CRP", "inflammation", true, false, []string{"c-reactive", "crp"}},
	{"hemoglobin", "Hemoglobin", "blood", false, false, []string{"hemoglobin", "haemoglobin"}},
	{"hematocrit", "Hematocrit", "blood", false, false, []string{"hematocrit", "haematocrit"}},
	{"rbc", "Red blood cells", "blood", false, false, []string{"red blood cell", "rbc count", "erythrocyte"}},
	{"mcv", "MCV", "blood", false, false, []string{"mean cell volume", "mcv", "mean corpuscular volume"}},
	{"mch", "MCH", "blood", false, false, []string{"mch"}},
	{"mchc", "MCHC", "blood", false, false, []string{"mchc"}},
	{"rdw", "RDW", "blood", false, false, []string{"distribution width", "rdw"}},
	{"wbc", "White blood cells", "blood", false, false, []string{"white blood cell", "wbc count", "leukocyte"}},
	{"neutrophils", "Neutrophils", "blood", false, false, []string{"neutrophil"}},
	{"lymphocytes", "Lymphocytes", "blood", false, false, []string{"lymphocyte"}},
	{"monocytes", "Monocytes", "blood", false, false, []string{"monocyte"}},
	{"eosinophils", "Eosinophils", "blood", false, false, []string{"eosinophil"}},
	{"basophils", "Basophils", "blood", false, false, []string{"basophil"}},
	{"platelets", "Platelets", "blood", false, false, []string{"platelet count", "platelets"}},
	{"mpv", "MPV", "blood", false, false, []string{"mean platelet", "mpv"}},
	{"psa", "PSA", "hormones", true, false, []string{"psa", "prostate"}},
}

var byCode = func() map[string]Definition {
	m := map[string]Definition{}
	for _, d := range Definitions {
		m[d.Code] = d
	}
	return m
}()

// Lookup returns the definition for a code.
func Lookup(code string) (Definition, bool) {
	d, ok := byCode[code]
	return d, ok
}

// Canonical maps a lab's test name to a code, or "" when unknown.
//
// MCH and MCHC share a prefix; the unit (pg vs g/L) tells them apart, and a
// name with "conc" is MCHC.
func Canonical(name, unit string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return ""
	}
	if strings.Contains(n, "mean cell hemoglobin") || strings.Contains(n, "mean corpuscular hemoglobin") || strings.HasPrefix(n, "mch") {
		u := strings.ToLower(unit)
		if strings.Contains(n, "conc") || strings.Contains(u, "g/l") || strings.Contains(u, "g/dl") || strings.HasPrefix(n, "mchc") {
			return "mchc"
		}
		return "mch"
	}
	if strings.Contains(n, "ratio") && strings.Contains(n, "creat") {
		return "acr"
	}
	if strings.Contains(n, "ldl") {
		return "ldl"
	}
	if strings.Contains(n, "non-hdl") || strings.Contains(n, "non hdl") {
		return "non_hdl"
	}
	if strings.Contains(n, "cholesterol/hdl") || (strings.Contains(n, "hdl") && strings.Contains(n, "ratio")) {
		return "chol_hdl_ratio"
	}
	for _, d := range Definitions {
		for _, a := range d.Aliases {
			if strings.Contains(n, a) {
				// "glucose" alone must not steal "fasting glucose".
				if d.Code == "glucose_random" && strings.Contains(n, "fast") {
					return "glucose_fasting"
				}
				if d.Code == "iron" && strings.Contains(n, "ferritin") {
					return "ferritin"
				}
				return d.Code
			}
		}
	}
	return ""
}

var (
	rangeBetween = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s*-\s*(-?\d+(?:\.\d+)?)`)
	rangeLess    = regexp.MustCompile(`^\s*<=?\s*(\d+(?:\.\d+)?)`)
	rangeMore    = regexp.MustCompile(`^\s*>=?\s*(\d+(?:\.\d+)?)`)
	numberLead   = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s*(.*)$`)
)

// ParseRange reads "129 - 165 g/L", "< 5.20 mmol/L", ">= 60 mL/min".
func ParseRange(s string) (low, high *float64, unit string) {
	s = strings.TrimSpace(s)
	if m := rangeBetween.FindStringSubmatch(s); m != nil {
		l, _ := strconv.ParseFloat(m[1], 64)
		h, _ := strconv.ParseFloat(m[2], 64)
		return &l, &h, strings.TrimSpace(s[len(m[0]):])
	}
	if m := rangeLess.FindStringSubmatch(s); m != nil {
		h, _ := strconv.ParseFloat(m[1], 64)
		return nil, &h, strings.TrimSpace(s[len(m[0]):])
	}
	if m := rangeMore.FindStringSubmatch(s); m != nil {
		l, _ := strconv.ParseFloat(m[1], 64)
		return &l, nil, strings.TrimSpace(strings.TrimLeft(s[len(m[0]):], "."))
	}
	return nil, nil, ""
}

// ParseResult reads "147 g/L    normal" into a value, unit and flag.
func ParseResult(s string) (value *float64, unit, flag string) {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	for _, f := range []string{"see details", "uninterpreted", "unavailable", "pending", "abnormal", "normal", "high", "low", "positive", "negative"} {
		if strings.HasSuffix(lower, f) {
			flag = strings.ReplaceAll(f, " ", "_")
			s = strings.TrimSpace(s[:len(s)-len(f)])
			break
		}
	}
	if m := numberLead.FindStringSubmatch(s); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			unit = strings.TrimSpace(m[2])
			return &v, unit, flag
		}
	}
	return nil, "", flag
}

// Flag derives a flag from a value and a range when the lab gave none.
func Flag(v *float64, low, high *float64) string {
	if v == nil {
		return ""
	}
	if high != nil && *v > *high {
		return "high"
	}
	if low != nil && *v < *low {
		return "low"
	}
	if low != nil || high != nil {
		return "normal"
	}
	return ""
}
