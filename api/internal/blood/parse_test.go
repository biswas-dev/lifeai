package blood

import "testing"

const dynacareSample = `
                NAME:    Example Patient                                            DATE SAMPLES COLLECTED:     2026 Aug 24, 12:28
                                                                                                  ORDERED BY:   Dr. Example Clinician
   Dynacare Plus
           1         HEMOGLOBIN                                                                     TEST STATUS
                                                                                                    Final
                                                                                                                        REFERENCE
                                                                                                                        129 - 165
                                                                                                                                             YOUR RESULT
                                                                                                             147 g/L    normal
REFERENCE RANGE: 129 - 165 g/L
          32         HEMOGLOBIN A1C (HB A1C)                                                        TEST STATUS
                                                                                                    Final
                                                                                                                                             YOUR RESULT
                                                                                                             7.1 %    high
REFERENCE RANGE: < 6.0 %
          30         ALANINE                                                                        TEST STATUS
                                                                                                    Final
                                                                                                                                             YOUR RESULT
                                                                                                             88 U/L    high
REFERENCE RANGE: < 46 U/L
          17         ESTIMATED GLOMERULAR                                                           TEST STATUS
                                                                                                    Final
                                                                                                                                             YOUR RESULT
                                                                                                             113 mL/min/1.73m*2   normal
REFERENCE RANGE: >= 60. mL/min/1.73m*2
           6         MEAN CELL HEMOGLOBIN                                                           TEST STATUS
                                                                                                    Final
                                                                                                                                             YOUR RESULT
                                                                                                             336 g/L    normal
REFERENCE RANGE: 313 - 344 g/L
          16        SMEAR:                                        TEST STATUS
                                                            Final
                                                                                                 YOUR RESULT
                                                                                                 See Details
`

func TestParseDynacare(t *testing.T) {
	rep := Parse(dynacareSample)
	if rep.TakenOn != "2026-08-24" || rep.Lab != "Dynacare" || rep.OrderedBy != "Dr. Example Clinician" {
		t.Fatalf("header wrong: %+v", rep)
	}
	byCode := map[string]Marker{}
	for _, m := range rep.Markers {
		byCode[m.Code] = m
	}
	a1c := byCode["hba1c"]
	if a1c.Value == nil || *a1c.Value != 7.1 || a1c.Unit != "%" || a1c.Flag != "high" || a1c.RefHigh == nil || *a1c.RefHigh != 6.0 {
		t.Fatalf("hba1c wrong: %+v", a1c)
	}
	alt := byCode["alt"]
	if alt.Name != "ALT" || alt.Value == nil || *alt.Value != 88 || alt.Flag != "high" {
		t.Fatalf("alt wrong: %+v", alt)
	}
	egfr := byCode["egfr"]
	if egfr.RefLow == nil || *egfr.RefLow != 60 || egfr.Unit != "mL/min/1.73m*2" {
		t.Fatalf("egfr wrong: %+v", egfr)
	}
	if _, ok := byCode["mchc"]; !ok {
		t.Fatalf("g/L mean cell hemoglobin should be MCHC: %v", byCode)
	}
	hb := byCode["hemoglobin"]
	if hb.RefLow == nil || *hb.RefLow != 129 || hb.Flag != "normal" {
		t.Fatalf("hemoglobin wrong: %+v", hb)
	}
	// The smear has no number but is kept as text.
	found := false
	for _, m := range rep.Markers {
		if m.Name == "SMEAR" && m.Flag == "see_details" && m.Value == nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("smear not kept: %+v", rep.Markers)
	}
}

func TestGenericFallback(t *testing.T) {
	rep := Parse("Lipid panel\nLDL Cholesterol    2.8   mmol/L    < 3.50\nHDL Cholesterol    1.3   mmol/L    >= 1.00\nTriglycerides   1.2  mmol/L   < 1.70  \nRandom text 42\n")
	if len(rep.Markers) != 3 {
		t.Fatalf("expected 3 markers, got %+v", rep.Markers)
	}
	if rep.Markers[0].Code != "ldl" || *rep.Markers[0].Value != 2.8 || *rep.Markers[0].RefHigh != 3.5 || rep.Markers[0].Flag != "normal" {
		t.Fatalf("ldl wrong: %+v", rep.Markers[0])
	}
}

func TestCanonical(t *testing.T) {
	cases := map[string]string{
		"CHOLESTEROL, TOTAL": "total_cholesterol", "NON-HDL-": "non_hdl", "THYROID STIMULATING": "tsh",
		"GLUCOSE, FASTING": "glucose_fasting", "Glucose Random": "glucose_random", "VITAMIN B12": "b12",
		"FERRITIN": "ferritin", "IRON": "iron", "Something odd": "", "ALBUMIN / CREATININE RATIO": "acr",
	}
	for in, want := range cases {
		if got := Canonical(in, ""); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePageBoundaryAndQualitativeMarkers(t *testing.T) {
	text := "\f24      TOTAL CHOLESTEROL/HDL    TEST STATUS\n Final\n REFERENCE     YOUR RESULT\n             4.2 uninterpreted\n RATIO\n\f36       Kidney Failure Risk Equation       TEST STATUS\n Final\n YOUR RESULT\n See Details\n"
	rep := Parse(text)
	if len(rep.Markers) != 2 {
		t.Fatalf("missing compact or mixed-case heading: %+v", rep)
	}
	m := rep.Markers[0]
	if m.Code != "chol_hdl_ratio" || m.Unit != "" || m.Flag != "uninterpreted" {
		t.Fatalf("misread ratio: %+v", m)
	}
	if rep.Markers[1].Flag != "see_details" || rep.Markers[1].Value != nil {
		t.Fatal("qualitative marker lost")
	}
}
