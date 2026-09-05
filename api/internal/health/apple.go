package health

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Apple Health export: Settings > Health > Export All Health Data gives an
// export.zip holding apple_health_export/export.xml, a flat list of Record
// and Workout elements. The file is routinely hundreds of megabytes, so it
// is streamed rather than loaded.

// AppleResult is what an export contained.
type AppleResult struct {
	Samples  []Sample
	Workouts []WorkoutImport
	Records  int
	Skipped  int
}

// ParseAppleZip finds export.xml inside the zip and parses it.
func ParseAppleZip(r io.ReaderAt, size int64) (*AppleResult, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("apple: not a zip: %w", err)
	}
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), "export.xml") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return ParseAppleXML(rc)
		}
	}
	return nil, fmt.Errorf("apple: export.xml not found in the zip")
}

type appleAgg struct {
	// per date -> per source -> value
	sum  map[string]map[string]float64
	last map[string]map[string]float64
}

func newAgg() *appleAgg {
	return &appleAgg{sum: map[string]map[string]float64{}, last: map[string]map[string]float64{}}
}

func (a *appleAgg) add(m map[string]map[string]float64, date, src string, v float64, replace bool) {
	if m[date] == nil {
		m[date] = map[string]float64{}
	}
	if replace {
		m[date][src] = v
	} else {
		m[date][src] += v
	}
}

// ParseAppleXML streams export.xml.
//
// Steps and sleep are summed per (day, device) and the largest device total
// is kept, because a phone and a watch each count the same steps and Health
// itself de-duplicates only in the app. Weight, body fat and resting heart
// rate take the last reading of the day.
func ParseAppleXML(r io.Reader) (*AppleResult, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	res := &AppleResult{}
	steps, sleep, water := newAgg(), newAgg(), newAgg()
	weight, fat, rhr := newAgg(), newAgg(), newAgg()
	seenWorkout := map[string]bool{}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("apple: reading xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		attrs := map[string]string{}
		for _, a := range se.Attr {
			attrs[a.Name.Local] = a.Value
		}
		switch se.Name.Local {
		case "Record":
			res.Records++
			typ := attrs["type"]
			src := attrs["sourceName"]
			v, err := strconv.ParseFloat(attrs["value"], 64)
			start, end := appleDate(attrs["startDate"]), appleDate(attrs["endDate"])
			switch typ {
			case "HKQuantityTypeIdentifierBodyMass":
				if err != nil {
					continue
				}
				if strings.EqualFold(attrs["unit"], "lb") {
					v = v / 2.20462
				}
				weight.add(weight.last, start, src, v, true)
			case "HKQuantityTypeIdentifierBodyFatPercentage":
				if err != nil {
					continue
				}
				if v <= 1 {
					v *= 100
				}
				fat.add(fat.last, start, src, v, true)
			case "HKQuantityTypeIdentifierRestingHeartRate":
				if err != nil {
					continue
				}
				rhr.add(rhr.last, start, src, v, true)
			case "HKQuantityTypeIdentifierStepCount":
				if err != nil {
					continue
				}
				steps.add(steps.sum, start, src, v, false)
			case "HKQuantityTypeIdentifierDietaryWater":
				if err != nil {
					continue
				}
				u := strings.ToLower(attrs["unit"])
				switch {
				case strings.HasPrefix(u, "l"):
					v *= 1000
				case strings.HasPrefix(u, "fl_oz"):
					v *= 29.5735
				}
				water.add(water.sum, start, src, v, false)
			case "HKCategoryTypeIdentifierSleepAnalysis":
				val := attrs["value"]
				if !strings.Contains(val, "Asleep") {
					continue
				}
				st, e1 := appleTime(attrs["startDate"])
				en, e2 := appleTime(attrs["endDate"])
				if e1 != nil || e2 != nil || !en.After(st) {
					continue
				}
				// A night's sleep belongs to the morning it ends on.
				sleep.add(sleep.sum, end, src, en.Sub(st).Hours(), false)
			default:
				res.Skipped++
			}
		case "Workout":
			res.Records++
			st, err := appleTime(attrs["startDate"])
			if err != nil {
				continue
			}
			dur, _ := strconv.ParseFloat(attrs["duration"], 64)
			if strings.HasPrefix(strings.ToLower(attrs["durationUnit"]), "sec") {
				dur /= 60
			} else if strings.HasPrefix(strings.ToLower(attrs["durationUnit"]), "h") {
				dur *= 60
			}
			mins := int(dur + 0.5)
			if mins <= 0 {
				continue
			}
			ext := "apple:" + attrs["startDate"]
			if seenWorkout[ext] {
				continue
			}
			seenWorkout[ext] = true
			w := WorkoutImport{
				Source: "apple", ExternalID: ext, Date: appleDate(attrs["startDate"]),
				Activity: appleActivity(attrs["workoutActivityType"]), Minutes: mins, StartedAt: st,
			}
			w.Kind = KindFromActivity(w.Activity)
			if kcal, err := strconv.ParseFloat(attrs["totalEnergyBurned"], 64); err == nil && kcal > 0 {
				if strings.EqualFold(attrs["totalEnergyBurnedUnit"], "kJ") {
					kcal /= 4.184
				}
				w.Kcal = &kcal
			}
			if d, err := strconv.ParseFloat(attrs["totalDistance"], 64); err == nil && d > 0 {
				u := strings.ToLower(attrs["totalDistanceUnit"])
				switch {
				case u == "mi":
					d *= 1.60934
				case u == "m":
					d /= 1000
				}
				w.DistanceKm = &d
			}
			res.Workouts = append(res.Workouts, w)
		}
	}

	best := func(a *appleAgg, m map[string]map[string]float64, metric string, maxOf bool) {
		for date, bySrc := range m {
			var v float64
			first := true
			for _, x := range bySrc {
				if first || (maxOf && x > v) || (!maxOf && x < v) {
					v, first = x, false
				}
			}
			if !first && v > 0 {
				res.Samples = append(res.Samples, Sample{Date: date, Metric: metric, Source: "apple", Value: round1(v)})
			}
		}
	}
	best(steps, steps.sum, MetricSteps, true)
	best(sleep, sleep.sum, MetricSleep, true)
	best(water, water.sum, MetricWater, true)
	best(weight, weight.last, MetricWeight, false)
	best(fat, fat.last, MetricBodyFat, false)
	best(rhr, rhr.last, MetricRestingHR, false)
	return res, nil
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// appleDate reads "2026-08-24 07:31:00 -0400" as the local calendar date.
func appleDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func appleTime(s string) (time.Time, error) {
	return time.Parse("2006-01-02 15:04:05 -0700", s)
}

func appleActivity(t string) string {
	t = strings.TrimPrefix(t, "HKWorkoutActivityType")
	// CamelCase to words: "TraditionalStrengthTraining" -> "Traditional Strength Training".
	var b strings.Builder
	for i, r := range t {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
