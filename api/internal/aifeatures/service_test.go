package aifeatures

import (
	"errors"
	"strings"
	"testing"
)

func TestReadEstimateSumsTotalsAndDropsJunk(t *testing.T) {
	est, err := readEstimate("```json\n{\"name\":\"Chicken bowl\",\"items\":[{\"name\":\"chicken\",\"kcal\":300,\"protein_g\":50},{\"name\":\"\",\"kcal\":10},{\"name\":\"rice\",\"kcal\":200,\"carbs_g\":45}]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(est.Items) != 2 || est.Kcal != 500 || est.ProteinG != 50 || est.CarbsG != 45 {
		t.Fatalf("unexpected estimate %+v", est)
	}
	if est.Items[0].Unit != "serving" || est.Items[0].Qty != 1 {
		t.Fatal("defaults not applied")
	}
}

func TestReadEstimateRejectsSchemaEcho(t *testing.T) {
	_, err := readEstimate(`{"name":"short dish name","items":[{"name":"ingredient","kcal":0}]}`)
	if !errors.Is(err, ErrNoEstimate) {
		t.Fatalf("expected ErrNoEstimate, got %v", err)
	}
	_, err = readEstimate(`{"name":"","items":[],"notes":"no food visible"}`)
	if err == nil || !strings.Contains(err.Error(), "no food visible") {
		t.Fatalf("expected the note in the error, got %v", err)
	}
}

func TestRenderHistoryMentionsTargets(t *testing.T) {
	out := RenderHistory(History{WindowDays: 14, DaysLogged: 10, AvgKcal: 2100, KcalTarget: 2000, WeightLatest: 82.5, WeightFirst: 84, TargetWeight: 80})
	for _, want := range []string{"14 days", "2100 kcal", "target of 2000", "-1.5 kg", "target 80.0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("brief missing %q:\n%s", want, out)
		}
	}
}

func TestDisabledService(t *testing.T) {
	var s *Service
	if s.Enabled() {
		t.Fatal("nil service should be disabled")
	}
	if New(nil).Enabled() {
		t.Fatal("nil chain should be disabled")
	}
}
