package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/dates"
	"github.com/biswas-dev/lifeai/api/internal/photo"
)

// Photo is a stored image as returned to the SPA. Paths on disk are never
// exposed — the client only ever gets ids and the URLs built from them.
type Photo struct {
	ID       int64  `json:"id"`
	Date     string `json:"date"`
	Kind     string `json:"kind"`
	Pose     string `json:"pose"`
	Caption  string `json:"caption"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Bytes    int64  `json:"bytes"`
	TakenAt  string `json:"taken_at"`
	Source   string `json:"source"`
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url"`
}

// HandleUploadPhoto accepts a multipart image, compresses and stores it.
//
// A food photo with autolog=1 also creates a meal and queues an estimate.
func (s *Server) HandleUploadPhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := UserID(ctx)

	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		respondError(w, http.StatusRequestEntityTooLarge, "image is too large", "too_large")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "a file field is required", "missing_file")
		return
	}
	defer file.Close()

	kind := strings.TrimSpace(r.FormValue("kind"))
	if kind == "" {
		kind = photo.KindProgress
	}
	if !photo.ValidKind(kind) {
		respondError(w, http.StatusBadRequest, "unknown photo kind", "invalid_kind")
		return
	}
	pose := strings.TrimSpace(r.FormValue("pose"))
	if !photo.ValidPose(pose) {
		respondError(w, http.StatusBadRequest, "unknown pose", "invalid_pose")
		return
	}
	date := strings.TrimSpace(r.FormValue("date"))
	if date == "" {
		date = s.today(ctx)
	}
	if !dates.Valid(date) {
		respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD", "invalid_date")
		return
	}

	saved, err := s.photos.Save(file, userID, kind, s.cfg.MaxUploadBytes)
	if err != nil {
		if errors.Is(err, photo.ErrUnsupportedType) {
			respondError(w, http.StatusBadRequest, "that file is not a supported image", "invalid_image")
			return
		}
		if errors.Is(err, photo.ErrTooLarge) {
			respondError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("that image is larger than the %d MB limit", s.cfg.MaxUploadBytes>>20), "image_too_large")
			return
		}
		s.log.Error("save photo", zap.Error(err), zap.String("filename", header.Filename))
		respondError(w, http.StatusInternalServerError, "could not save image", "internal")
		return
	}

	caption := strings.TrimSpace(r.FormValue("caption"))
	if err := s.ensureDay(ctx, userID, date); err != nil {
		s.photos.Remove(saved.RelPath, saved.ThumbPath)
		respondError(w, http.StatusInternalServerError, "could not save image", "internal")
		return
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO photos (user_id, on_date, kind, pose, rel_path, thumb_path, mime, width, height, bytes, sha256, caption)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, date, kind, pose, saved.RelPath, saved.ThumbPath, saved.Mime,
		saved.Width, saved.Height, saved.Bytes, saved.SHA256, caption)
	if err != nil {
		s.photos.Remove(saved.RelPath, saved.ThumbPath)
		s.log.Error("insert photo", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not save image", "internal")
		return
	}
	id, _ := res.LastInsertId()

	if kind == photo.KindFood && isTrue(r.FormValue("autolog")) {
		s.queueFoodEstimate(ctx, userID, id, date, saved.RelPath, r.FormValue("slot"), caption)
	}

	p, err := s.photoByID(ctx, userID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load image", "internal")
		return
	}
	respondJSON(w, http.StatusCreated, p)
}

// HandleServePhoto streams a stored image. Ownership is checked on every
// request — the id alone is never authorisation.
func (s *Server) HandleServePhoto(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "photoID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid photo id", "invalid_id")
		return
	}
	var relPath, thumbPath, mime string
	err = s.db.QueryRowContext(r.Context(),
		`SELECT rel_path, thumb_path, mime FROM photos WHERE id = ? AND user_id = ?`,
		id, UserID(r.Context())).Scan(&relPath, &thumbPath, &mime)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "photo not found", "not_found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load photo", "internal")
		return
	}
	path := relPath
	if r.URL.Query().Get("size") == "thumb" && thumbPath != "" {
		path = thumbPath
	}
	f, err := s.photos.Open(path)
	if err != nil {
		respondError(w, http.StatusNotFound, "photo file missing", "not_found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if info, err := f.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	if _, err := io.Copy(w, f); err != nil {
		s.log.Warn("stream photo", zap.Error(err))
	}
}

// HandleListPhotos returns the caller's photos, newest first.
func (s *Server) HandleListPhotos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	query := `SELECT id, on_date, kind, pose, caption, width, height, bytes, taken_at, source FROM photos WHERE user_id = ?`
	args := []any{UserID(ctx)}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		if !photo.ValidKind(kind) {
			respondError(w, http.StatusBadRequest, "unknown photo kind", "invalid_kind")
			return
		}
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	if pose := r.URL.Query().Get("pose"); pose != "" {
		if !photo.ValidPose(pose) {
			respondError(w, http.StatusBadRequest, "unknown pose", "invalid_pose")
			return
		}
		query += ` AND pose = ?`
		args = append(args, pose)
	}
	query += ` ORDER BY on_date DESC, taken_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not list photos", "internal")
		return
	}
	defer rows.Close()
	out := []Photo{}
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "could not read photos", "internal")
			return
		}
		out = append(out, p)
	}
	respondJSON(w, http.StatusOK, out)
}

type updatePhotoRequest struct {
	Pose    *string `json:"pose"`
	Caption *string `json:"caption"`
	Date    *string `json:"date"`
}

// HandleUpdatePhoto edits the tags on a photo.
func (s *Server) HandleUpdatePhoto(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "photoID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid photo id", "invalid_id")
		return
	}
	var req updatePhotoRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	if req.Pose != nil {
		if !photo.ValidPose(*req.Pose) {
			respondError(w, http.StatusBadRequest, "unknown pose", "invalid_pose")
			return
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE photos SET pose = ? WHERE id = ? AND user_id = ?`, *req.Pose, id, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update photo", "internal")
			return
		}
	}
	if req.Caption != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE photos SET caption = ? WHERE id = ? AND user_id = ?`, strings.TrimSpace(*req.Caption), id, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update photo", "internal")
			return
		}
	}
	if req.Date != nil {
		if !dates.Valid(*req.Date) {
			respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD", "invalid_date")
			return
		}
		_ = s.ensureDay(ctx, userID, *req.Date)
		if _, err := s.db.ExecContext(ctx, `UPDATE photos SET on_date = ? WHERE id = ? AND user_id = ?`, *req.Date, id, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update photo", "internal")
			return
		}
	}
	p, err := s.photoByID(ctx, userID, id)
	if err != nil {
		respondError(w, http.StatusNotFound, "photo not found", "not_found")
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// HandleDeletePhoto removes a photo and its file.
func (s *Server) HandleDeletePhoto(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "photoID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid photo id", "invalid_id")
		return
	}
	ctx := r.Context()
	var relPath, thumbPath string
	if err := s.db.QueryRowContext(ctx,
		`SELECT rel_path, thumb_path FROM photos WHERE id = ? AND user_id = ?`, id, UserID(ctx)).
		Scan(&relPath, &thumbPath); err != nil {
		respondError(w, http.StatusNotFound, "photo not found", "not_found")
		return
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM photos WHERE id = ? AND user_id = ?`, id, UserID(ctx)); err != nil {
		respondError(w, http.StatusInternalServerError, "could not delete photo", "internal")
		return
	}
	s.photos.Remove(relPath, thumbPath)
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

const photoColumns = `id, on_date, kind, pose, caption, width, height, bytes, taken_at, source`

func (s *Server) photoByID(ctx context.Context, userID, id int64) (Photo, error) {
	return scanPhoto(s.db.QueryRowContext(ctx,
		`SELECT `+photoColumns+` FROM photos WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Server) photosForDate(ctx context.Context, userID int64, date string) ([]Photo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+photoColumns+` FROM photos WHERE user_id = ? AND on_date = ? ORDER BY taken_at DESC, id DESC`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Photo{}
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanPhoto(row scanner) (Photo, error) {
	var p Photo
	if err := row.Scan(&p.ID, &p.Date, &p.Kind, &p.Pose, &p.Caption, &p.Width, &p.Height, &p.Bytes, &p.TakenAt, &p.Source); err != nil {
		return p, err
	}
	p.URL = "/api/photos/" + strconv.FormatInt(p.ID, 10) + "/file"
	p.ThumbURL = p.URL + "?size=thumb"
	return p, nil
}

func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
