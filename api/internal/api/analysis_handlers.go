package api

import (
	"context"
	"net/http"
	"time"

	"github.com/biswas-dev/lifeai/api/internal/dates"
)

// HealthSummary is the whole picture, computed rather than generated: it is
// what an MCP client reads before doing its own analysis, so nothing here
// costs a model call.
type HealthSummary struct {
	GeneratedAt string `json:"generated_at"`
	Profile     struct {
		Name     string   `json:"name"`
		Age      *int     `json:"age,omitempty"`
		Sex      string   `json:"sex"`
		HeightCm *float64 `json:"height_cm,omitempty"`
		WeightKg *float64 `json:"weight_kg,omitempty"`
		BMI      *float64 `json:"bmi,omitempty"`
		Timezone string   `json:"timezone"`
	} `json:"profile"`
	Goals Goals `json:"goals"`
	Blood struct {
		LatestReportDate string         `json:"latest_report_date,omitempty"`
		Reports          int            `json:"reports"`
		NextTestDue      string         `json:"next_test_due,omitempty"`
		Watch            []MarkerSeries `json:"watch"`
		Abnormal         []MarkerSeries `json:"abnormal"`
	} `json:"blood"`
	Window30 Stats `json:"window_30d"`
	Window90 struct {
		WeightTrend    Trend   `json:"weight_trend"`
		AvgKcal        float64 `json:"avg_kcal"`
		AvgProtein     float64 `json:"avg_protein"`
		WorkoutCount   int     `json:"workout_count"`
		WorkoutMinutes int     `json:"workout_minutes"`
		DaysLogged     int     `json:"days_logged"`
	} `json:"window_90d"`
	Today   Day      `json:"today"`
	Signals []string `json:"signals"`
}

// HandleHealthSummary returns the computed summary.
func (s *Server) HandleHealthSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.healthSummary(r.Context(), UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not build summary", "internal")
		return
	}
	respondJSON(w, http.StatusOK, sum)
}

func (s *Server) healthSummary(ctx context.Context, userID int64) (HealthSummary, error) {
	var sum HealthSummary
	sum.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	user, err := s.getUser(ctx, userID)
	if err != nil {
		return sum, err
	}
	sum.Profile.Name, sum.Profile.Sex, sum.Profile.HeightCm, sum.Profile.Timezone = user.Name, user.Sex, user.HeightCm, user.Timezone
	if user.DOB != "" {
		if dob, err := time.Parse("2006-01-02", user.DOB); err == nil {
			now := time.Now()
			age := now.Year() - dob.Year()
			if now.Month() < dob.Month() || (now.Month() == dob.Month() && now.Day() < dob.Day()) {
				age--
			}
			sum.Profile.Age = &age
		}
	}
	if sum.Goals, err = s.goals(ctx, userID); err != nil {
		return sum, err
	}
	if sum.Today, err = s.loadDay(ctx, userID, s.today(ctx)); err != nil {
		return sum, err
	}
	if sum.Window30, err = s.stats(ctx, userID, 30); err != nil {
		return sum, err
	}
	st90, err := s.stats(ctx, userID, 90)
	if err != nil {
		return sum, err
	}
	sum.Window90.WeightTrend, sum.Window90.AvgKcal, sum.Window90.AvgProtein = st90.WeightTrend, st90.AvgKcal, st90.AvgProtein
	sum.Window90.WorkoutCount, sum.Window90.WorkoutMinutes, sum.Window90.DaysLogged = st90.WorkoutCount, st90.WorkoutMinutes, st90.DaysLogged

	// Latest weight from any point in the record, not just the window.
	var w float64
	if err := s.db.QueryRowContext(ctx, `SELECT weight_kg FROM days WHERE user_id = ? AND weight_kg IS NOT NULL ORDER BY on_date DESC LIMIT 1`, userID).Scan(&w); err == nil {
		sum.Profile.WeightKg = &w
		if user.HeightCm != nil && *user.HeightCm > 0 {
			m := *user.HeightCm / 100
			bmi := w / (m * m)
			bmi = float64(int(bmi*10+0.5)) / 10
			sum.Profile.BMI = &bmi
		}
	}

	series, err := s.markerSeries(ctx, userID)
	if err != nil {
		return sum, err
	}
	sum.Blood.Watch, sum.Blood.Abnormal = []MarkerSeries{}, []MarkerSeries{}
	for _, ms := range series {
		if ms.Watch {
			sum.Blood.Watch = append(sum.Blood.Watch, ms)
		}
		if ms.Flag == "high" || ms.Flag == "low" || ms.Flag == "abnormal" {
			sum.Blood.Abnormal = append(sum.Blood.Abnormal, ms)
		}
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(taken_on), '') FROM blood_reports WHERE user_id = ?`, userID).Scan(&sum.Blood.Reports, &sum.Blood.LatestReportDate)
	if sum.Blood.LatestReportDate != "" {
		sum.Blood.NextTestDue = dates.AddDays(sum.Blood.LatestReportDate, 90)
	}

	sum.Signals = signals(sum)
	return sum, nil
}

// signals are plain-language observations derived from the numbers. Not
// advice: they say what the record shows, which is what a coach or a model
// reads before saying anything.
func signals(sum HealthSummary) []string {
	var out []string
	for _, ms := range sum.Blood.Abnormal {
		if ms.Latest == nil {
			continue
		}
		dir := "above"
		if ms.Flag == "low" {
			dir = "below"
		}
		msg := ms.Name + " is " + dir + " range (" + trimFloat(ms.Latest.Value) + " " + ms.Unit
		if ms.RefHigh != nil && ms.Flag == "high" {
			msg += ", range < " + trimFloat(*ms.RefHigh)
		}
		if ms.RefLow != nil && ms.Flag == "low" {
			msg += ", range > " + trimFloat(*ms.RefLow)
		}
		msg += ") as of " + ms.Latest.Date
		if ms.Change != nil {
			msg += "; change since first report " + signed(*ms.Change)
		}
		out = append(out, msg+".")
	}
	st := sum.Window30
	if st.DaysLogged > 0 {
		if sum.Goals.DailyKcal != nil && st.AvgKcal > 0 {
			diff := st.AvgKcal - float64(*sum.Goals.DailyKcal)
			if diff > 150 {
				out = append(out, "Average intake over 30 days is "+trimFloat(diff)+" kcal above target.")
			} else if diff < -150 {
				out = append(out, "Average intake over 30 days is "+trimFloat(-diff)+" kcal below target.")
			}
		}
		if sum.Goals.ProteinG != nil && st.AvgProtein > 0 && st.AvgProtein < float64(*sum.Goals.ProteinG)*0.85 {
			out = append(out, "Protein averages "+trimFloat(st.AvgProtein)+" g against a "+trimFloat(float64(*sum.Goals.ProteinG))+" g target.")
		}
		perWeek := float64(st.WorkoutCount) / float64(st.DaysInWindow) * 7
		if perWeek < 3 {
			out = append(out, "Training averages "+trimFloat(perWeek)+" sessions a week over 30 days.")
		}
		if st.AvgSleep > 0 && st.AvgSleep < 6.5 {
			out = append(out, "Sleep averages "+trimFloat(st.AvgSleep)+" hours.")
		}
		if st.AvgSteps > 0 && sum.Goals.Steps != nil && st.AvgSteps < float64(*sum.Goals.Steps)*0.8 {
			out = append(out, "Steps average "+trimFloat(st.AvgSteps)+" against a "+trimFloat(float64(*sum.Goals.Steps))+" target.")
		}
		if st.WeightTrend.Change != nil && st.WeightTrend.Count >= 3 {
			out = append(out, "Weight changed "+signed(*st.WeightTrend.Change)+" kg over the last 30 days.")
		}
	} else {
		out = append(out, "Nothing logged in the last 30 days.")
	}
	if sum.Blood.NextTestDue != "" {
		out = append(out, "90-day review milestone: "+sum.Blood.NextTestDue+" (confirm retest timing with your clinician).")
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func trimFloat(v float64) string {
	s := formatFloat(v, 2)
	return s
}

func signed(v float64) string {
	if v > 0 {
		return "+" + trimFloat(v)
	}
	return trimFloat(v)
}

func formatFloat(v float64, prec int) string {
	// Two decimals at most, trailing zeros trimmed.
	s := fmtFloat(v, prec)
	return s
}
