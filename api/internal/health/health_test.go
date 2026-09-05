package health

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
)

const appleXML = `<?xml version="1.0" encoding="UTF-8"?>
<HealthData locale="en_CA">
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Withings" unit="kg" startDate="2026-08-24 07:31:00 -0400" endDate="2026-08-24 07:31:00 -0400" value="81.3"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Withings" unit="lb" startDate="2026-08-25 07:31:00 -0400" endDate="2026-08-25 07:31:00 -0400" value="178"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="iPhone" unit="count" startDate="2026-08-24 08:00:00 -0400" endDate="2026-08-24 08:10:00 -0400" value="1200"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="iPhone" unit="count" startDate="2026-08-24 09:00:00 -0400" endDate="2026-08-24 09:10:00 -0400" value="800"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="Watch" unit="count" startDate="2026-08-24 08:00:00 -0400" endDate="2026-08-24 08:10:00 -0400" value="2500"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Watch" startDate="2026-08-23 23:00:00 -0400" endDate="2026-08-24 03:00:00 -0400" value="HKCategoryValueSleepAnalysisAsleepCore"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Watch" startDate="2026-08-24 03:00:00 -0400" endDate="2026-08-24 06:30:00 -0400" value="HKCategoryValueSleepAnalysisAsleepDeep"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Watch" startDate="2026-08-23 22:30:00 -0400" endDate="2026-08-23 23:00:00 -0400" value="HKCategoryValueSleepAnalysisInBed"/>
 <Record type="HKQuantityTypeIdentifierRestingHeartRate" sourceName="Watch" unit="count/min" startDate="2026-08-24 00:00:00 -0400" endDate="2026-08-24 23:59:00 -0400" value="57"/>
 <Workout workoutActivityType="HKWorkoutActivityTypeRunning" duration="32.5" durationUnit="min" totalDistance="5.2" totalDistanceUnit="km" totalEnergyBurned="310" totalEnergyBurnedUnit="kcal" sourceName="Watch" startDate="2026-08-24 18:00:00 -0400" endDate="2026-08-24 18:32:30 -0400"/>
</HealthData>`

func TestParseAppleXML(t *testing.T) {
	res, err := ParseAppleXML(strings.NewReader(appleXML))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, s := range res.Samples {
		got[s.Date+"/"+s.Metric] = s.Value
	}
	if got["2026-08-24/weight_kg"] != 81.3 {
		t.Fatalf("weight: %v", got)
	}
	if v := got["2026-08-25/weight_kg"]; v < 80.7 || v > 80.8 {
		t.Fatalf("lb weight not converted: %v", v)
	}
	// Watch 2500 beats phone 2000; never 4500.
	if got["2026-08-24/steps"] != 2500 {
		t.Fatalf("steps should take the best device, got %v", got["2026-08-24/steps"])
	}
	if got["2026-08-24/sleep_hours"] != 7.5 {
		t.Fatalf("sleep: %v", got["2026-08-24/sleep_hours"])
	}
	if got["2026-08-24/resting_hr"] != 57 {
		t.Fatalf("rhr: %v", got)
	}
	if len(res.Workouts) != 1 {
		t.Fatalf("workouts: %+v", res.Workouts)
	}
	w := res.Workouts[0]
	if w.Kind != "run" || w.Minutes != 33 || *w.Kcal != 310 || *w.DistanceKm != 5.2 || w.Date != "2026-08-24" {
		t.Fatalf("workout: %+v", w)
	}
}

func TestParseAppleZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("apple_health_export/export.xml")
	_, _ = f.Write([]byte(appleXML))
	zw.Close()
	res, err := ParseAppleZip(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil || len(res.Samples) == 0 {
		t.Fatalf("zip: %v %+v", err, res)
	}
}

func TestParseSamsungZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("com.samsung.shealth.tracker.pedometer_day_summary.20260901.csv")
	_, _ = f.Write([]byte("com.samsung.shealth.tracker.pedometer_day_summary,4,1\nday_time,count,calorie,distance,source_type,datauuid\n2026-08-24 00:00:00.000,8421,300,6000,-2,abc\n2026-08-24 00:00:00.000,8100,290,5900,1,def\n"))
	f, _ = zw.Create("com.samsung.health.weight.20260901.csv")
	_, _ = f.Write([]byte("com.samsung.health.weight,5,1\nstart_time,weight,body_fat,datauuid\n2026-08-24 07:30:00.000,81.5,22.4,w1\n"))
	f, _ = zw.Create("com.samsung.shealth.exercise.20260901.csv")
	_, _ = f.Write([]byte("com.samsung.shealth.exercise,6,1\ncom.samsung.health.exercise.start_time,com.samsung.health.exercise.exercise_type,com.samsung.health.exercise.duration,com.samsung.health.exercise.calorie,com.samsung.health.exercise.distance,com.samsung.health.exercise.mean_heart_rate,com.samsung.health.exercise.datauuid\n2026-08-24 18:00:30.000,1002,1950000,305,5150,151,ex1\n"))
	zw.Close()
	res, err := ParseSamsungZip(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, s := range res.Samples {
		got[s.Date+"/"+s.Metric] = s.Value
	}
	if got["2026-08-24/steps"] != 8421 || got["2026-08-24/weight_kg"] != 81.5 || got["2026-08-24/body_fat_pct"] != 22.4 {
		t.Fatalf("samples: %v", got)
	}
	if len(res.Workouts) != 1 || res.Workouts[0].Kind != "run" || res.Workouts[0].Minutes != 33 || *res.Workouts[0].AvgHR != 151 {
		t.Fatalf("workout: %+v", res.Workouts)
	}
}

func TestSameWorkoutDedup(t *testing.T) {
	a := time.Date(2026, 8, 24, 22, 0, 0, 0, time.UTC)
	b := a.Add(30 * time.Second)
	if !SameWorkout(a, 33, b, 32) {
		t.Fatal("watch and phone should match")
	}
	if SameWorkout(a, 33, a.Add(10*time.Minute), 33) {
		t.Fatal("ten minutes apart is a different workout")
	}
	if SameWorkout(a, 30, b, 60) {
		t.Fatal("double the duration is a different workout")
	}
}

func TestWebhookShapes(t *testing.T) {
	res, err := ParseWebhook([]byte(`{"source":"garmin","metrics":[{"date":"2026-08-24","metric":"steps","value":9000},{"date":"2026-08-24","metric":"bogus","value":1}],"workouts":[{"started_at":"2026-08-24T18:00:00Z","minutes":40,"activity":"Trail run","kcal":400}]}`), "webhook")
	if err != nil || res.Source != "garmin" || len(res.Samples) != 1 || len(res.Workouts) != 1 || res.Workouts[0].Kind != "run" {
		t.Fatalf("generic: %v %+v", err, res)
	}
	hae := `{"data":{"metrics":[{"name":"weight_body_mass","units":"kg","data":[{"date":"2026-08-24 07:31:00 -0400","qty":81.2}]},{"name":"step_count","units":"count","data":[{"date":"2026-08-24 08:00:00 -0400","qty":500},{"date":"2026-08-24 09:00:00 -0400","qty":700}]}],"workouts":[{"name":"Outdoor Walk","start":"2026-08-24 12:00:00 -0400","end":"2026-08-24 12:45:00 -0400","duration":2700,"activeEnergy":{"qty":180},"distance":{"qty":3.1,"units":"mi"}}]}}`
	res, err = ParseWebhook([]byte(hae), "webhook")
	if err != nil || res.Source != "apple" {
		t.Fatalf("hae: %v %+v", err, res)
	}
	got := map[string]float64{}
	for _, s := range res.Samples {
		got[s.Metric] = s.Value
	}
	if got["weight_kg"] != 81.2 || got["steps"] != 1200 {
		t.Fatalf("hae samples: %v", got)
	}
	if len(res.Workouts) != 1 || res.Workouts[0].Minutes != 45 || res.Workouts[0].Kind != "walk" || *res.Workouts[0].DistanceKm < 4.9 {
		t.Fatalf("hae workout: %+v", res.Workouts)
	}
}
