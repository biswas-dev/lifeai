package health

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Samsung Health export: Settings > Download personal data gives a zip of
// CSV files, one per data type, each with a title line above the header:
//
//	com.samsung.health.weight,5,1
//	start_time,weight,body_fat,...
//
// Column sets vary by app version, so columns are found by name.

// SamsungResult is what an export contained.
type SamsungResult struct {
	Samples  []Sample
	Workouts []WorkoutImport
	Files    []string
	Skipped  int
}

// ParseSamsungZip reads every CSV it recognises.
func ParseSamsungZip(r io.ReaderAt, size int64) (*SamsungResult, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("samsung: not a zip: %w", err)
	}
	res := &SamsungResult{}
	steps := map[string]float64{}
	sleep := map[string]float64{}
	water := map[string]float64{}
	weight := map[string]float64{}
	fat := map[string]float64{}
	rhr := map[string]float64{}
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if !strings.HasSuffix(name, ".csv") {
			continue
		}
		base := name[strings.LastIndex(name, "/")+1:]
		kind := ""
		switch {
		case strings.Contains(base, "pedometer_day_summary"):
			kind = "steps"
		case strings.Contains(base, "health.weight"):
			kind = "weight"
		case strings.Contains(base, "shealth.sleep.") || strings.Contains(base, "health.sleep."):
			kind = "sleep"
		case strings.Contains(base, "water_intake"):
			kind = "water"
		case strings.Contains(base, "shealth.exercise.") || strings.Contains(base, "health.exercise."):
			kind = "exercise"
		case strings.Contains(base, "rest_heart_rate") || strings.Contains(base, "resting_heart_rate"):
			kind = "rhr"
		default:
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		rows, err := readSamsungCSV(rc)
		rc.Close()
		if err != nil {
			res.Skipped++
			continue
		}
		res.Files = append(res.Files, base)
		for _, row := range rows {
			switch kind {
			case "steps":
				d := samsungDate(row, "day_time", "create_time", "start_time")
				if v := num(row, "count", "step_count"); d != "" && v > steps[d] {
					steps[d] = v
				}
			case "weight":
				d := samsungDate(row, "start_time", "create_time")
				if v := num(row, "weight"); d != "" && v > 0 {
					weight[d] = v
				}
				if v := num(row, "body_fat", "body_fat_mass"); d != "" && v > 0 && v < 70 {
					fat[d] = v
				}
			case "sleep":
				// Sleep ends in the morning; attribute the night to the end date.
				d := samsungDate(row, "end_time", "start_time")
				mins := num(row, "sleep_duration")
				if mins == 0 {
					if st, en := samsungTime(row, "start_time"), samsungTime(row, "end_time"); !st.IsZero() && en.After(st) {
						mins = en.Sub(st).Minutes()
					}
				}
				if d != "" && mins > 0 {
					sleep[d] += mins / 60
				}
			case "water":
				d := samsungDate(row, "start_time", "create_time")
				if v := num(row, "amount"); d != "" && v > 0 {
					water[d] += v
				}
			case "rhr":
				d := samsungDate(row, "start_time", "create_time")
				if v := num(row, "heart_rate"); d != "" && v > 0 {
					rhr[d] = v
				}
			case "exercise":
				st := samsungTime(row, "start_time")
				if st.IsZero() {
					continue
				}
				mins := int(num(row, "duration")/60000 + 0.5)
				if mins <= 0 {
					continue
				}
				ext := "samsung:" + first(row, "datauuid")
				if ext == "samsung:" {
					ext = "samsung:" + st.Format(time.RFC3339)
				}
				activity := samsungExercise(int(num(row, "exercise_type")))
				if t := first(row, "title"); t != "" && !strings.HasPrefix(t, "%") {
					activity = t
				}
				w := WorkoutImport{Source: "samsung", ExternalID: ext, Date: st.Format("2006-01-02"), Activity: activity,
					Kind: KindFromActivity(activity), Minutes: mins, StartedAt: st}
				if v := num(row, "calorie"); v > 0 {
					w.Kcal = &v
				}
				if v := num(row, "distance"); v > 0 {
					km := v / 1000
					w.DistanceKm = &km
				}
				if v := num(row, "mean_heart_rate"); v > 0 {
					hr := int(v + 0.5)
					w.AvgHR = &hr
				}
				res.Workouts = append(res.Workouts, w)
			}
		}
	}
	add := func(m map[string]float64, metric string) {
		for d, v := range m {
			res.Samples = append(res.Samples, Sample{Date: d, Metric: metric, Source: "samsung", Value: round1(v)})
		}
	}
	add(steps, MetricSteps)
	add(sleep, MetricSleep)
	add(water, MetricWater)
	add(weight, MetricWeight)
	add(fat, MetricBodyFat)
	add(rhr, MetricRestingHR)
	return res, nil
}

// readSamsungCSV returns rows as name->value maps, skipping the title line.
func readSamsungCSV(r io.Reader) ([]map[string]string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	var header []string
	var rows []map[string]string
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header == nil {
			// The first line is "com.samsung.health.weight,5,1"; the header
			// is the first line with more than three columns.
			if len(rec) <= 3 {
				continue
			}
			header = make([]string, len(rec))
			for i, h := range rec {
				h = strings.ToLower(strings.TrimSpace(h))
				// Columns are often prefixed: com.samsung.health.exercise.duration
				if i := strings.LastIndex(h, "."); i >= 0 {
					h = h[i+1:]
				}
				header[i] = h
			}
			continue
		}
		row := map[string]string{}
		for i, v := range rec {
			if i < len(header) && header[i] != "" {
				row[header[i]] = strings.TrimSpace(v)
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func first(row map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := row[k]; v != "" {
			return v
		}
	}
	return ""
}

func num(row map[string]string, keys ...string) float64 {
	v, _ := strconv.ParseFloat(first(row, keys...), 64)
	return v
}

// samsungTime reads "2026-08-24 07:31:00.000" or epoch milliseconds.
func samsungTime(row map[string]string, keys ...string) time.Time {
	s := first(row, keys...)
	if s == "" {
		return time.Time{}
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		if ms > 1e12 {
			return time.UnixMilli(ms).UTC()
		}
		return time.Unix(ms, 0).UTC()
	}
	for _, layout := range []string{"2006-01-02 15:04:05.000", "2006-01-02 15:04:05", "2006-01-02T15:04:05.000", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func samsungDate(row map[string]string, keys ...string) string {
	t := samsungTime(row, keys...)
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// samsungExercise names the numeric exercise_type codes Samsung uses.
func samsungExercise(code int) string {
	switch code {
	case 1001:
		return "Walking"
	case 1002:
		return "Running"
	case 11007:
		return "Cycling"
	case 13001:
		return "Hiking"
	case 14001:
		return "Swimming"
	case 15006:
		return "Elliptical"
	case 15005, 10007:
		return "Strength training"
	case 9002:
		return "Yoga"
	case 10004:
		return "Pilates"
	case 15002:
		return "Treadmill"
	case 15003:
		return "Exercise bike"
	case 15004:
		return "Rowing machine"
	case 6003:
		return "Tennis"
	case 4004:
		return "Football"
	case 0:
		return "Workout"
	}
	return "Workout"
}
