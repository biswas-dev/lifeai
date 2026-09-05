package health

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Webhook payloads. Two shapes are accepted: lifeai's own, and the one the
// iOS app "Health Auto Export" posts, so a phone can push its day to
// /api/import/health on a schedule without an export file.

// GenericPayload is lifeai's own shape.
type GenericPayload struct {
	Source  string `json:"source"`
	Metrics []struct {
		Date   string  `json:"date"`
		Metric string  `json:"metric"`
		Value  float64 `json:"value"`
	} `json:"metrics"`
	Workouts []struct {
		ExternalID string   `json:"external_id"`
		StartedAt  string   `json:"started_at"`
		Minutes    int      `json:"minutes"`
		Kind       string   `json:"kind"`
		Activity   string   `json:"activity"`
		Kcal       *float64 `json:"kcal"`
		DistanceKm *float64 `json:"distance_km"`
		AvgHR      *int     `json:"avg_hr"`
		Notes      string   `json:"notes"`
	} `json:"workouts"`
}

// WebhookResult is what a payload contained.
type WebhookResult struct {
	Source   string
	Samples  []Sample
	Workouts []WorkoutImport
}

// ParseWebhook reads either shape.
func ParseWebhook(body []byte, defaultSource string) (*WebhookResult, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("webhook: not a JSON object: %w", err)
	}
	if _, ok := probe["data"]; ok {
		return parseHealthAutoExport(body, defaultSource)
	}
	var p GenericPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("webhook: bad payload: %w", err)
	}
	src := strings.ToLower(strings.TrimSpace(p.Source))
	if src == "" {
		src = defaultSource
	}
	res := &WebhookResult{Source: src}
	for _, m := range p.Metrics {
		if !ValidMetric(m.Metric) || len(m.Date) < 10 {
			continue
		}
		res.Samples = append(res.Samples, Sample{Date: m.Date[:10], Metric: m.Metric, Source: src, Value: m.Value})
	}
	for _, w := range p.Workouts {
		st, err := time.Parse(time.RFC3339, w.StartedAt)
		if err != nil || w.Minutes <= 0 {
			continue
		}
		ext := w.ExternalID
		if ext == "" {
			ext = st.UTC().Format(time.RFC3339)
		}
		kind := w.Kind
		if kind == "" {
			kind = KindFromActivity(w.Activity)
		}
		res.Workouts = append(res.Workouts, WorkoutImport{Source: src, ExternalID: src + ":" + ext, Date: st.Format("2006-01-02"),
			Kind: kind, Activity: w.Activity, Minutes: w.Minutes, Kcal: w.Kcal, DistanceKm: w.DistanceKm, AvgHR: w.AvgHR, StartedAt: st, Notes: w.Notes})
	}
	return res, nil
}

// Health Auto Export: {"data":{"metrics":[{"name":"weight_body_mass","units":"kg","data":[{"date":"2026-08-24 07:31:00 -0400","qty":81.2}]}],"workouts":[...]}}
func parseHealthAutoExport(body []byte, defaultSource string) (*WebhookResult, error) {
	var p struct {
		Data struct {
			Metrics []struct {
				Name  string `json:"name"`
				Units string `json:"units"`
				Data  []struct {
					Date string  `json:"date"`
					Qty  float64 `json:"qty"`
					// Sleep exports break the night down.
					Asleep float64 `json:"asleep"`
					Total  float64 `json:"totalSleep"`
				} `json:"data"`
			} `json:"metrics"`
			Workouts []struct {
				Name   string  `json:"name"`
				Start  string  `json:"start"`
				End    string  `json:"end"`
				Dur    float64 `json:"duration"`
				Energy struct {
					Qty float64 `json:"qty"`
				} `json:"activeEnergy"`
				Distance struct {
					Qty   float64 `json:"qty"`
					Units string  `json:"units"`
				} `json:"distance"`
				HR struct {
					Avg float64 `json:"avg"`
				} `json:"heartRate"`
			} `json:"workouts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("webhook: bad Health Auto Export payload: %w", err)
	}
	src := "apple"
	if defaultSource != "" && defaultSource != "webhook" {
		src = defaultSource
	}
	res := &WebhookResult{Source: src}
	agg := map[string]map[string]float64{}
	put := func(metric, date string, v float64, sum bool) {
		if agg[metric] == nil {
			agg[metric] = map[string]float64{}
		}
		if sum {
			agg[metric][date] += v
		} else {
			agg[metric][date] = v
		}
	}
	for _, m := range p.Data.Metrics {
		for _, d := range m.Data {
			if len(d.Date) < 10 {
				continue
			}
			date := d.Date[:10]
			switch strings.ToLower(m.Name) {
			case "weight_body_mass", "body_mass", "weight":
				v := d.Qty
				if strings.EqualFold(m.Units, "lb") {
					v /= 2.20462
				}
				put(MetricWeight, date, v, false)
			case "body_fat_percentage":
				v := d.Qty
				if v <= 1 {
					v *= 100
				}
				put(MetricBodyFat, date, v, false)
			case "resting_heart_rate":
				put(MetricRestingHR, date, d.Qty, false)
			case "step_count", "steps":
				put(MetricSteps, date, d.Qty, true)
			case "sleep_analysis":
				h := d.Asleep
				if h == 0 {
					h = d.Total
				}
				if h == 0 {
					h = d.Qty
				}
				if strings.HasPrefix(strings.ToLower(m.Units), "min") {
					h /= 60
				}
				put(MetricSleep, date, h, true)
			case "dietary_water", "water":
				v := d.Qty
				if strings.EqualFold(m.Units, "L") {
					v *= 1000
				}
				put(MetricWater, date, v, true)
			}
		}
	}
	for metric, byDate := range agg {
		for date, v := range byDate {
			if v > 0 {
				res.Samples = append(res.Samples, Sample{Date: date, Metric: metric, Source: src, Value: round1(v)})
			}
		}
	}
	for _, w := range p.Data.Workouts {
		st, err := time.Parse("2006-01-02 15:04:05 -0700", w.Start)
		if err != nil {
			continue
		}
		mins := int(w.Dur/60 + 0.5)
		if mins <= 0 {
			if en, err := time.Parse("2006-01-02 15:04:05 -0700", w.End); err == nil {
				mins = int(en.Sub(st).Minutes() + 0.5)
			}
		}
		if mins <= 0 {
			continue
		}
		wi := WorkoutImport{Source: src, ExternalID: src + ":" + w.Start, Date: st.Format("2006-01-02"), Activity: w.Name,
			Kind: KindFromActivity(w.Name), Minutes: mins, StartedAt: st}
		if w.Energy.Qty > 0 {
			k := w.Energy.Qty
			wi.Kcal = &k
		}
		if w.Distance.Qty > 0 {
			d := w.Distance.Qty
			if strings.EqualFold(w.Distance.Units, "mi") {
				d *= 1.60934
			} else if strings.EqualFold(w.Distance.Units, "m") {
				d /= 1000
			}
			wi.DistanceKm = &d
		}
		if w.HR.Avg > 0 {
			hr := int(w.HR.Avg + 0.5)
			wi.AvgHR = &hr
		}
		res.Workouts = append(res.Workouts, wi)
	}
	return res, nil
}
