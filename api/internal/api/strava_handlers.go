package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/health"
	"github.com/biswas-dev/lifeai/api/internal/integrations/strava"
)

// StravaStatus is the connection as shown in settings.
type StravaStatus struct {
	Configured bool    `json:"configured"`
	Connected  bool    `json:"connected"`
	Username   string  `json:"username"`
	AthleteID  int64   `json:"athlete_id"`
	LastSyncAt *string `json:"last_sync_at"`
	LastError  string  `json:"last_error"`
	Imported   int     `json:"imported"`
}

func (s *Server) strava() *strava.Client {
	return strava.New(s.cfg.StravaClientID, s.cfg.StravaClientSecret)
}

// HandleStravaStatus reports the connection.
func (s *Server) HandleStravaStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.stravaStatus(r.Context(), UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load strava", "internal")
		return
	}
	respondJSON(w, http.StatusOK, st)
}

func (s *Server) stravaStatus(ctx context.Context, userID int64) (StravaStatus, error) {
	st := StravaStatus{Configured: s.strava().Configured() && s.cipher.Enabled()}
	var last sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT username, athlete_id, last_sync_at, last_error, imported FROM strava_accounts WHERE user_id = ?`, userID).
		Scan(&st.Username, &st.AthleteID, &last, &st.LastError, &st.Imported)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Connected = true
	st.LastSyncAt = strPtr(last)
	return st, nil
}

// HandleStravaConnect returns the consent URL. The state is an HMAC over the
// user id and a nonce, so the callback can trust who it is for.
func (s *Server) HandleStravaConnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if TokenAuth(ctx) {
		respondError(w, http.StatusForbidden, "sign in to change integrations", "token_forbidden")
		return
	}
	if !s.strava().Configured() {
		respondError(w, http.StatusServiceUnavailable, "this server has no Strava application configured", "strava_unconfigured")
		return
	}
	if !s.cipher.Enabled() {
		respondError(w, http.StatusServiceUnavailable, "this server has no ENCRYPTION_KEY, so it cannot store Strava tokens", "no_encryption_key")
		return
	}
	state := s.stravaState(UserID(ctx))
	respondJSON(w, http.StatusOK, map[string]string{"url": s.strava().AuthorizeURL(s.cfg.AppURL+"/api/strava/callback", state)})
}

func (s *Server) stravaState(userID int64) string {
	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	exp := time.Now().Add(15 * time.Minute).Unix()
	payload := strconv.FormatInt(userID, 10) + ":" + hex.EncodeToString(nonce) + ":" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret+":strava"))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + hex.EncodeToString(mac.Sum(nil))))
}

func (s *Server) parseStravaState(state string) (int64, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return 0, false
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 4 {
		return 0, false
	}
	payload := strings.Join(parts[:3], ":")
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret+":strava"))
	mac.Write([]byte(payload))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(parts[3])) {
		return 0, false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, false
	}
	userID, err := strconv.ParseInt(parts[0], 10, 64)
	return userID, err == nil
}

// HandleStravaCallback finishes the consent flow. Unauthenticated: it is a
// browser redirect carrying the signed state.
func (s *Server) HandleStravaCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		http.Redirect(w, r, s.cfg.AppURL+"/app/settings?strava=error&reason="+strings.ReplaceAll(msg, " ", "+"), http.StatusFound)
	}
	userID, ok := s.parseStravaState(r.URL.Query().Get("state"))
	if !ok {
		fail("invalid state")
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		fail(e)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		fail("no code")
		return
	}
	tok, err := s.strava().Exchange(r.Context(), code)
	if err != nil {
		s.log.Warn("strava exchange", zap.Error(err))
		fail("token exchange failed")
		return
	}
	if !strings.Contains(tok.Scope, "activity:read") {
		fail("activity permission was not granted")
		return
	}
	accessEnc, err1 := s.cipher.Seal(tok.AccessToken)
	refreshEnc, err2 := s.cipher.Seal(tok.RefreshToken)
	if err1 != nil || err2 != nil {
		fail("could not store tokens")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO strava_accounts (user_id, athlete_id, username, access_token_enc, refresh_token_enc, expires_at, scope, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, '')
		ON CONFLICT(user_id) DO UPDATE SET athlete_id = excluded.athlete_id, username = excluded.username, access_token_enc = excluded.access_token_enc,
			refresh_token_enc = excluded.refresh_token_enc, expires_at = excluded.expires_at, scope = excluded.scope, last_error = '', updated_at = CURRENT_TIMESTAMP`,
		userID, tok.Athlete.ID, tok.Athlete.Username, accessEnc, refreshEnc, tok.ExpiresAt, tok.Scope); err != nil {
		fail("could not save connection")
		return
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if _, err := s.syncStrava(bg, userID); err != nil {
			s.log.Warn("strava first sync", zap.Int64("user", userID), zap.Error(err))
		}
	}()
	http.Redirect(w, r, s.cfg.AppURL+"/app/settings?strava=connected", http.StatusFound)
}

// HandleStravaSync pulls new activities now.
func (s *Server) HandleStravaSync(w http.ResponseWriter, r *http.Request) {
	n, err := s.syncStrava(r.Context(), UserID(r.Context()))
	if err != nil {
		if errors.Is(err, errStravaNotConnected) {
			respondError(w, http.StatusBadRequest, "Strava is not connected", "not_connected")
			return
		}
		respondError(w, http.StatusBadGateway, "Strava sync failed: "+err.Error(), "strava_failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"imported": n})
}

// HandleStravaDisconnect forgets the tokens. Imported workouts stay.
func (s *Server) HandleStravaDisconnect(w http.ResponseWriter, r *http.Request) {
	if TokenAuth(r.Context()) {
		respondError(w, http.StatusForbidden, "sign in to change integrations", "token_forbidden")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM strava_accounts WHERE user_id = ?`, UserID(r.Context())); err != nil {
		respondError(w, http.StatusInternalServerError, "could not disconnect", "internal")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

var errStravaNotConnected = errors.New("strava not connected")

var stravaMu sync.Mutex

// syncStrava imports activities since the last sync (or the last 90 days
// on first connect), de-duplicating against sessions seen by other devices.
func (s *Server) syncStrava(ctx context.Context, userID int64) (int, error) {
	stravaMu.Lock()
	defer stravaMu.Unlock()
	var (
		accessEnc, refreshEnc string
		expiresAt             int64
		lastSync              sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT access_token_enc, refresh_token_enc, expires_at, last_sync_at FROM strava_accounts WHERE user_id = ?`, userID).
		Scan(&accessEnc, &refreshEnc, &expiresAt, &lastSync)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errStravaNotConnected
	}
	if err != nil {
		return 0, err
	}
	access, err := s.cipher.Open(accessEnc)
	if err != nil {
		return 0, err
	}
	client := s.strava()
	if (strava.Token{ExpiresAt: expiresAt}).Expired() {
		refresh, err := s.cipher.Open(refreshEnc)
		if err != nil {
			return 0, err
		}
		tok, err := client.Refresh(ctx, refresh)
		if err != nil {
			s.setStravaError(ctx, userID, err.Error())
			return 0, err
		}
		access = tok.AccessToken
		a, _ := s.cipher.Seal(tok.AccessToken)
		rf, _ := s.cipher.Seal(tok.RefreshToken)
		_, _ = s.db.ExecContext(ctx, `UPDATE strava_accounts SET access_token_enc = ?, refresh_token_enc = ?, expires_at = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`, a, rf, tok.ExpiresAt, userID)
	}
	after := time.Now().AddDate(0, 0, -90)
	if lastSync.Valid {
		if t, ok := parseDBTime(lastSync.String); ok {
			// Overlap a day so an activity uploaded late is not missed.
			after = t.Add(-24 * time.Hour)
		}
	}
	imported := 0
	for page := 1; page <= 10; page++ {
		acts, err := client.Activities(ctx, access, after, page, 100)
		if err != nil {
			s.setStravaError(ctx, userID, err.Error())
			return imported, err
		}
		for _, a := range acts {
			mins := a.Minutes()
			if mins <= 0 {
				continue
			}
			activity := a.Name
			if activity == "" {
				activity = a.SportType
			}
			wk := health.WorkoutImport{
				Source: "strava", ExternalID: strconv.FormatInt(a.ID, 10), Date: a.LocalDate(),
				Kind: health.KindFromActivity(a.SportType + " " + a.Name), Activity: activity, Minutes: mins, StartedAt: a.StartTime(),
			}
			if wk.Kind == "other" && a.Trainer {
				wk.Kind = "cardio"
			}
			if a.Calories != nil && *a.Calories > 0 {
				wk.Kcal = a.Calories
			}
			if a.Distance > 0 {
				km := a.Distance / 1000
				wk.DistanceKm = &km
			}
			if a.AverageHR != nil && *a.AverageHR > 0 {
				hr := int(*a.AverageHR + 0.5)
				wk.AvgHR = &hr
			}
			_ = s.ensureDay(ctx, userID, wk.Date)
			ins, err := s.importWorkout(ctx, userID, wk)
			if err != nil {
				return imported, err
			}
			if ins {
				imported++
			}
		}
		if len(acts) < 100 {
			break
		}
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE strava_accounts SET last_sync_at = CURRENT_TIMESTAMP, last_error = '', imported = imported + ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`, imported, userID)
	return imported, nil
}

func (s *Server) setStravaError(ctx context.Context, userID int64, msg string) {
	if len(msg) > 300 {
		msg = msg[:300]
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE strava_accounts SET last_error = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`, msg, userID)
}

// StravaSyncer polls every connected account.
type StravaSyncer struct {
	s        *Server
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
}

// NewStravaSyncer builds the poller.
func NewStravaSyncer(s *Server, interval time.Duration) *StravaSyncer {
	if interval < 5*time.Minute {
		interval = 5 * time.Minute
	}
	return &StravaSyncer{s: s, interval: interval, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start runs the loop.
func (p *StravaSyncer) Start(ctx context.Context) {
	go func() {
		defer close(p.done)
		t := time.NewTicker(p.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.stop:
				return
			case <-t.C:
				rows, err := p.s.db.QueryContext(ctx, `SELECT user_id FROM strava_accounts`)
				if err != nil {
					continue
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
					if _, err := p.s.syncStrava(ctx, id); err != nil {
						p.s.log.Warn("strava poll", zap.Int64("user", id), zap.Error(err))
					}
				}
			}
		}
	}()
}

// Stop ends the loop.
func (p *StravaSyncer) Stop() {
	close(p.stop)
	<-p.done
}
