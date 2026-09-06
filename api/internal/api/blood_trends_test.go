package api

import (
	"encoding/json"
	"testing"
)

func TestBloodHistoryPreservesEachRangeAcrossYears(t *testing.T) {
	_, handler := newTestServer(t)
	owner := signup(t, handler, "history@example.com")
	other := signup(t, handler, "other-history@example.com")
	other.do("POST", "/api/blood/reports", map[string]any{"taken_on": "2027-01-01", "markers": []map[string]any{{"name": "HbA1c", "value": 99, "unit": "%"}}})
	// Deliberately insert dates out of order, including a marker outside the catalog.
	for _, row := range []map[string]any{
		{"taken_on": "2026-09-01", "markers": []map[string]any{{"name": "HbA1c", "value": 7, "unit": "%", "ref_high": 8, "ref_text": "< 8 %"}, {"name": "Custom marker", "value": 4, "unit": "U/L"}}},
		{"taken_on": "2023-09-01", "markers": []map[string]any{{"name": "HbA1c", "value": 7.1, "unit": "%", "ref_high": 6, "ref_text": "< 6 %"}, {"name": "Custom marker", "value": 2, "unit": "U/L"}}},
	} {
		if status, body := owner.do("POST", "/api/blood/reports", row); status != 201 {
			t.Fatalf("create report: %d %v", status, body)
		}
	}
	status, response := owner.doList("GET", "/api/blood/markers")
	if status != 200 || len(response) != 2 {
		t.Fatalf("marker history missing: %d %+v", status, response)
	}
	raw, _ := json.Marshal(response)
	var series []MarkerSeries
	if err := json.Unmarshal(raw, &series); err != nil {
		t.Fatal(err)
	}
	a1c := series[0]
	if len(a1c.Points) != 2 || a1c.Points[0].Date != "2023-09-01" || a1c.Points[1].Date != "2026-09-01" {
		t.Fatalf("dates or owner isolation wrong: %+v", a1c)
	}
	first, last := a1c.Points[0], a1c.Points[1]
	if first.RefHigh == nil || *first.RefHigh != 6 || first.RefText != "< 6 %" || first.Flag != "high" || first.RefLow != nil {
		t.Fatalf("historical reference lost: %+v", first)
	}
	if last.RefHigh == nil || *last.RefHigh != 8 || last.RefText != "< 8 %" || last.Flag != "normal" || a1c.RefText != last.RefText {
		t.Fatalf("latest reference wrong: %+v", last)
	}
	if series[1].Name != "Custom marker" || len(series[1].Points) != 2 || series[1].Change == nil || *series[1].Change != 2 {
		t.Fatalf("unknown numeric marker was not tracked: %+v", series[1])
	}
}
