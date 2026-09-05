package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/dates"
	"github.com/biswas-dev/lifeai/api/internal/health"
	"github.com/biswas-dev/lifeai/api/internal/integrations/hard75"
	"github.com/biswas-dev/lifeai/api/internal/photo"
)

// The 75hard bridge. One way, once a day: everything 75hard holds for the
// connected account — body photos, food photos and meals, weight and resting
// heart rate, workouts, meditation and journal — is pulled into lifeai and
// keyed by its 75hard id, so a nightly run never duplicates a row.
//
// Rows that came from 75hard are marked source='75hard' and are refreshed on
// every pull. Body metrics are the exception: a value typed into lifeai by
// hand is never overwritten by an import.

const hard75Provider = "75hard"

// Hard75Pace is the gap between requests to 75hard. A variable so tests can
// run the pull without waiting out a real rate limit.
var Hard75Pace = 650 * time.Millisecond

// SyncSummary is what one pull brought in.
type SyncSummary struct {
	Programs    int    `json:"programs"`
	Days        int    `json:"days"`
	Photos      int    `json:"photos"`
	Meals       int    `json:"meals"`
	Workouts    int    `json:"workouts"`
	Meditations int    `json:"meditations"`
	Journal     int    `json:"journal"`
	Requests    int    `json:"requests"`
	DurationSec int    `json:"duration_sec"`
	FinishedAt  string `json:"finished_at"`
}

// Hard75Syncer runs the pull for every connected account on a timer.
type Hard75Syncer struct {
	s        *Server
	interval time.Duration
	mu       sync.Mutex
	running  map[int64]bool
	stop     chan struct{}
	done     chan struct{}
}

// NewHard75Syncer builds a syncer.
func NewHard75Syncer(s *Server, interval time.Duration) *Hard75Syncer {
	return &Hard75Syncer{s: s, interval: interval, running: map[int64]bool{}, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start runs a pull shortly after boot for anything overdue, then on the
// interval. It never runs the same account twice at once.
func (h *Hard75Syncer) Start(ctx context.Context) {
	go func() {
		defer close(h.done)
		timer := time.NewTimer(2 * time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.stop:
				return
			case <-timer.C:
				h.runDue(ctx)
				timer.Reset(h.interval / 4)
			}
		}
	}()
}

// Stop ends the loop.
func (h *Hard75Syncer) Stop() {
	close(h.stop)
	<-h.done
}

// runDue pulls every enabled connection whose last pull is older than the
// interval.
func (h *Hard75Syncer) runDue(ctx context.Context) {
	rows, err := h.s.db.QueryContext(ctx, `
		SELECT i.user_id FROM integrations i JOIN users u ON u.id = i.user_id
		 WHERE i.provider = ? AND i.enabled = 1 AND u.deleted_at IS NULL
		   AND (i.last_sync_at IS NULL OR i.last_sync_at < ?)`,
		hard75Provider, time.Now().UTC().Add(-h.interval))
	if err != nil {
		h.s.log.Warn("75hard: list due", zap.Error(err))
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if _, err := h.SyncUser(ctx, id); err != nil {
			h.s.log.Warn("75hard: scheduled pull failed", zap.Int64("user", id), zap.Error(err))
		}
	}
}

// ErrSyncRunning means a pull for this account is already in progress.
var ErrSyncRunning = errors.New("a 75hard pull is already running for this account")

// SyncUser pulls one account now. Safe to call from a request: the work is
// bounded by the pace of the 75hard client, and it records its outcome on the
// integrations row either way.
func (h *Hard75Syncer) SyncUser(ctx context.Context, userID int64) (*SyncSummary, error) {
	h.mu.Lock()
	if h.running[userID] {
		h.mu.Unlock()
		return nil, ErrSyncRunning
	}
	h.running[userID] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.running, userID)
		h.mu.Unlock()
	}()

	s := h.s
	var email string
	if err := s.db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = ? AND deleted_at IS NULL`, userID).Scan(&email); err != nil {
		return nil, err
	}
	if !s.cfg.Hard75Allowed(email) {
		return nil, errors.New("this account is not allowed to use the 75hard bridge")
	}
	baseURL, token, err := s.hard75Credentials(ctx, userID)
	if err != nil {
		return nil, err
	}

	_, _ = s.db.ExecContext(ctx, `UPDATE integrations SET last_status = 'running', updated_at = CURRENT_TIMESTAMP WHERE user_id = ? AND provider = ?`, userID, hard75Provider)
	start := time.Now()
	client := hard75.New(baseURL, token)
	client.Pace = Hard75Pace
	summary, err := h.pull(ctx, userID, client)
	summary.DurationSec = int(time.Since(start).Seconds())
	summary.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	sumJSON, _ := json.Marshal(summary)
	status, errText := "ok", ""
	if err != nil {
		status, errText = "failed", err.Error()
		if errors.Is(err, hard75.ErrUnauthorized) {
			errText = "75hard rejected the token; create a new one and reconnect"
		}
		if len(errText) > 500 {
			errText = errText[:500]
		}
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE integrations SET last_sync_at = CURRENT_TIMESTAMP, last_status = ?, last_error = ?, last_summary = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE user_id = ? AND provider = ?`, status, errText, string(sumJSON), userID, hard75Provider)
	s.log.Info("75hard: pull finished", zap.Int64("user", userID), zap.String("status", status),
		zap.Int("days", summary.Days), zap.Int("photos", summary.Photos), zap.Int("meals", summary.Meals), zap.Error(err))
	return &summary, err
}

func (s *Server) hard75Credentials(ctx context.Context, userID int64) (string, string, error) {
	var baseURL, enc string
	var enabled bool
	err := s.db.QueryRowContext(ctx, `SELECT base_url, token_enc, enabled FROM integrations WHERE user_id = ? AND provider = ?`, userID, hard75Provider).
		Scan(&baseURL, &enc, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errors.New("75hard is not connected")
	}
	if err != nil {
		return "", "", err
	}
	if !enabled {
		return "", "", errors.New("the 75hard connection is paused")
	}
	token, err := s.cipher.Open(enc)
	if err != nil {
		return "", "", fmt.Errorf("could not read the stored token: %w", err)
	}
	return baseURL, token, nil
}

// pull does the work. It is written to be re-runnable: every insert is keyed
// by the 75hard id, and a failure part way leaves a consistent prefix.
func (h *Hard75Syncer) pull(ctx context.Context, userID int64, c *hard75.Client) (SyncSummary, error) {
	s := h.s
	var sum SyncSummary
	today := dates.Today(s.userLocation(context.WithValue(ctx, UserIDKey, userID)))

	programs, err := c.Programs(ctx)
	if err != nil {
		return sum, err
	}
	sum.Requests++
	sum.Programs = len(programs)

	// Photos first: meals and days point at them.
	photoMap, err := h.pullPhotos(ctx, userID, c, &sum)
	if err != nil {
		return sum, err
	}

	// Which day number of which program is which date, for the journal.
	dateFor := map[string]string{}

	for _, p := range programs {
		days, err := c.Days(ctx, p.ID)
		if err != nil {
			return sum, err
		}
		sum.Requests++
		for _, d := range days {
			dateFor[fmt.Sprintf("%d:%d", p.ID, d.DayNumber)] = d.Date
			if d.Date > today {
				continue
			}
			// A day with nothing done and no photo has nothing to give, and
			// skipping it keeps a 75-day pull to the days that matter.
			if d.TasksDone == 0 && d.PhotoCount == 0 && d.Status == "pending" {
				continue
			}
			day, err := c.Day(ctx, p.ID, d.DayNumber)
			if err != nil {
				return sum, err
			}
			sum.Requests++
			if err := h.importDay(ctx, userID, day, photoMap, &sum); err != nil {
				return sum, fmt.Errorf("day %s: %w", d.Date, err)
			}
		}
	}

	entries, err := c.Journal(ctx)
	if err != nil {
		return sum, err
	}
	sum.Requests++
	for _, e := range entries {
		date := ""
		if e.DayNumber != nil {
			for _, p := range programs {
				if d, ok := dateFor[fmt.Sprintf("%d:%d", p.ID, *e.DayNumber)]; ok {
					date = d
					break
				}
			}
		}
		if date == "" {
			if t, err := time.Parse(time.RFC3339, e.CreatedAt); err == nil {
				date = dates.Local(t, s.userLocation(context.WithValue(ctx, UserIDKey, userID)))
			} else if len(e.CreatedAt) >= 10 && dates.Valid(e.CreatedAt[:10]) {
				date = e.CreatedAt[:10]
			} else {
				date = today
			}
		}
		body := strings.TrimSpace(e.Body)
		if body == "" {
			body = strings.TrimSpace(e.ParsedText)
		}
		if body == "" {
			continue
		}
		if err := s.ensureDay(ctx, userID, date); err != nil {
			return sum, err
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO journal_entries (user_id, on_date, title, body, source, external_id)
			VALUES (?, ?, ?, ?, '75hard', ?)
			ON CONFLICT(user_id, source, external_id) WHERE external_id <> '' DO UPDATE SET
				title = excluded.title, body = excluded.body, on_date = excluded.on_date, updated_at = CURRENT_TIMESTAMP`,
			userID, date, strings.TrimSpace(e.Title), body, "journal:"+strconv.FormatInt(e.ID, 10)); err != nil {
			return sum, err
		}
		sum.Journal++
	}
	return sum, nil
}

// pullPhotos imports every photo not seen before and returns the map from
// 75hard photo id to lifeai photo id, including ones imported earlier.
func (h *Hard75Syncer) pullPhotos(ctx context.Context, userID int64, c *hard75.Client, sum *SyncSummary) (map[int64]int64, error) {
	s := h.s
	out := map[int64]int64{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, external_id FROM photos WHERE user_id = ? AND source = '75hard'`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var ext string
		if rows.Scan(&id, &ext) == nil {
			if n, err := strconv.ParseInt(strings.TrimPrefix(ext, "photo:"), 10, 64); err == nil {
				out[n] = id
			}
		}
	}
	rows.Close()

	photos, err := c.Photos(ctx, "", 1000)
	if err != nil {
		return nil, err
	}
	sum.Requests++
	loc := s.userLocation(context.WithValue(ctx, UserIDKey, userID))
	for _, p := range photos {
		if _, seen := out[p.ID]; seen {
			continue
		}
		data, _, err := c.PhotoBytes(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		sum.Requests++
		kind := p.Kind
		if !photo.ValidKind(kind) {
			kind = photo.KindProgress
		}
		pose := p.Pose
		if !photo.ValidPose(pose) {
			pose = ""
		}
		date := ""
		var takenAt any
		if t, err := time.Parse(time.RFC3339, p.TakenAt); err == nil {
			date = dates.Local(t, loc)
			takenAt = t.UTC()
		} else if len(p.TakenAt) >= 10 && dates.Valid(p.TakenAt[:10]) {
			date = p.TakenAt[:10]
			takenAt = p.TakenAt
		} else {
			date = dates.Today(loc)
			takenAt = time.Now().UTC()
		}
		saved, err := s.photos.SaveBytes(data, userID, kind, int64(len(data))+1)
		if err != nil {
			s.log.Warn("75hard: photo rejected", zap.Int64("photo", p.ID), zap.Error(err))
			continue
		}
		if err := s.ensureDay(ctx, userID, date); err != nil {
			return nil, err
		}
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO photos (user_id, on_date, kind, pose, rel_path, thumb_path, mime, width, height, bytes, sha256, caption, taken_at, source, external_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '75hard', ?)`,
			userID, date, kind, pose, saved.RelPath, saved.ThumbPath, saved.Mime, saved.Width, saved.Height, saved.Bytes,
			saved.SHA256, strings.TrimSpace(p.Caption), takenAt, "photo:"+strconv.FormatInt(p.ID, 10))
		if err != nil {
			s.photos.Remove(saved.RelPath, saved.ThumbPath)
			return nil, err
		}
		id, _ := res.LastInsertId()
		out[p.ID] = id
		sum.Photos++
	}
	return out, nil
}

func (h *Hard75Syncer) importDay(ctx context.Context, userID int64, d *hard75.Day, photoMap map[int64]int64, sum *SyncSummary) error {
	s := h.s
	if !dates.Valid(d.Date) {
		return nil
	}
	// Metrics: fill gaps only. A number typed into lifeai wins.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO days (user_id, on_date, weight_kg, resting_hr, note, source)
		VALUES (?, ?, ?, ?, ?, '75hard')
		ON CONFLICT(user_id, on_date) DO UPDATE SET
			weight_kg  = COALESCE(days.weight_kg, excluded.weight_kg),
			resting_hr = COALESCE(days.resting_hr, excluded.resting_hr),
			note       = CASE WHEN days.note = '' THEN excluded.note ELSE days.note END,
			updated_at = CURRENT_TIMESTAMP`,
		userID, d.Date, nullFloat(d.WeightKg), nullInt(d.RestingHR), strings.TrimSpace(d.Note)); err != nil {
		return err
	}
	sum.Days++

	for _, m := range d.Meals {
		if m.EstimateStatus == "pending" || m.EstimateStatus == "failed" {
			// Not priced yet at the source; it will come through on a later pull.
			continue
		}
		var photoID any
		if m.PhotoID != nil {
			if id, ok := photoMap[*m.PhotoID]; ok {
				photoID = id
			}
		}
		slot := m.Slot
		if !validSlot(slot) {
			slot = "snack"
		}
		var eatenAt any = d.Date + " 12:00:00"
		if t, err := time.Parse(time.RFC3339, m.EatenAt); err == nil {
			eatenAt = t.UTC()
		}
		notes := strings.TrimSpace(m.Notes)
		ext := "meal:" + strconv.FormatInt(m.ID, 10)
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO meals (user_id, on_date, photo_id, name, slot, kcal, protein_g, carbs_g, fat_g, source, notes, eaten_at, estimate_status, external_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '75hard', ?, ?, '', ?)
			ON CONFLICT(user_id, source, external_id) WHERE external_id <> '' DO UPDATE SET
				on_date = excluded.on_date, photo_id = COALESCE(excluded.photo_id, meals.photo_id), name = excluded.name, slot = excluded.slot,
				kcal = excluded.kcal, protein_g = excluded.protein_g, carbs_g = excluded.carbs_g, fat_g = excluded.fat_g,
				notes = excluded.notes, eaten_at = excluded.eaten_at, updated_at = CURRENT_TIMESTAMP`,
			userID, d.Date, photoID, strings.TrimSpace(m.Name), slot, m.Kcal, m.ProteinG, m.CarbsG, m.FatG, notes, eatenAt, ext)
		if err != nil {
			return err
		}
		// LastInsertId is meaningless after an upsert that updated, so the
		// row is always looked up by its 75hard id.
		var mealID int64
		_ = s.db.QueryRowContext(ctx, `SELECT id FROM meals WHERE user_id = ? AND source = '75hard' AND external_id = ?`, userID, ext).Scan(&mealID)
		if mealID != 0 {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM meal_items WHERE meal_id = ?`, mealID); err != nil {
				return err
			}
			for i, it := range m.Items {
				if strings.TrimSpace(it.Name) == "" {
					continue
				}
				if _, err := s.db.ExecContext(ctx, `
					INSERT INTO meal_items (meal_id, name, qty, unit, kcal, protein_g, carbs_g, fat_g, confidence, sort_order)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					mealID, strings.TrimSpace(it.Name), it.Qty, it.Unit, it.Kcal, it.ProteinG, it.CarbsG, it.FatG, nullFloat(it.Confidence), i); err != nil {
					return err
				}
			}
		}
		sum.Meals++
	}

	for _, w := range d.Workouts {
		started, _ := time.Parse(time.RFC3339, w.StartedAt)
		_, err := s.importWorkout(ctx, userID, health.WorkoutImport{
			Source: "75hard", ExternalID: "workout:" + strconv.FormatInt(w.ID, 10),
			Date: d.Date, Kind: MapWorkoutKind(w.Kind, w.Activity), Activity: w.Activity,
			Minutes: w.Minutes, Kcal: w.Kcal, Notes: w.Notes, StartedAt: started,
		})
		if err != nil {
			return err
		}
		sum.Workouts++
	}

	for _, m := range d.Meditations {
		var started any
		if t, err := time.Parse(time.RFC3339, m.StartedAt); err == nil {
			started = t.UTC()
		}
		notes := strings.TrimSpace(m.Notes)
		if r := strings.TrimSpace(m.Reflection); r != "" {
			if notes != "" {
				notes += "\n\n"
			}
			notes += r
		}
		style := m.Style
		if style == "" {
			style = "guided"
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO meditations (user_id, on_date, minutes, style, notes, started_at, source, external_id)
			VALUES (?, ?, ?, ?, ?, ?, '75hard', ?)
			ON CONFLICT(user_id, source, external_id) WHERE external_id <> '' DO UPDATE SET
				on_date = excluded.on_date, minutes = excluded.minutes, style = excluded.style, notes = excluded.notes, started_at = excluded.started_at`,
			userID, d.Date, m.Minutes, style, notes, started, "meditation:"+strconv.FormatInt(m.ID, 10)); err != nil {
			return err
		}
		sum.Meditations++
	}

	// Photos are already imported; make sure they sit on the day 75hard put
	// them on, which is the program day rather than the upload timestamp.
	for _, p := range d.Photos {
		if id, ok := photoMap[p.ID]; ok {
			_, _ = s.db.ExecContext(ctx, `UPDATE photos SET on_date = ? WHERE id = ? AND user_id = ? AND source = '75hard'`, d.Date, id, userID)
		}
	}
	return nil
}

// MapWorkoutKind turns a 75hard workout (indoor/outdoor plus a free-text
// activity) into one of lifeai's kinds.
func MapWorkoutKind(kind, activity string) string {
	a := strings.ToLower(activity)
	switch {
	case strings.Contains(a, "walk") || strings.Contains(a, "hike"):
		return "walk"
	case strings.Contains(a, "run") || strings.Contains(a, "jog"):
		return "run"
	case strings.Contains(a, "cycl") || strings.Contains(a, "bike") || strings.Contains(a, "ride") || strings.Contains(a, "spin"):
		return "cycle"
	case strings.Contains(a, "swim"):
		return "swim"
	case strings.Contains(a, "yoga") || strings.Contains(a, "stretch") || strings.Contains(a, "pilates") || strings.Contains(a, "mobility"):
		return "yoga"
	case strings.Contains(a, "lift") || strings.Contains(a, "weight") || strings.Contains(a, "strength") || strings.Contains(a, "gym") || strings.Contains(a, "push") || strings.Contains(a, "pull") || strings.Contains(a, "squat"):
		return "strength"
	case strings.Contains(a, "hiit") || strings.Contains(a, "circuit") || strings.Contains(a, "crossfit"):
		return "hiit"
	case strings.Contains(a, "tennis") || strings.Contains(a, "football") || strings.Contains(a, "soccer") || strings.Contains(a, "basketball") || strings.Contains(a, "badminton") || strings.Contains(a, "squash") || strings.Contains(a, "golf"):
		return "sport"
	}
	if strings.Contains(strings.ToLower(kind), "outdoor") {
		return "cardio"
	}
	return "other"
}
