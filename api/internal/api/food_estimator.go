package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/aifeatures"
)

// AISlotTimeout bounds one provider attempt so the fallback stays reachable.
const AISlotTimeout = 60 * time.Second

// AIMaxAttempts is the retry budget per provider.
const AIMaxAttempts = 2

type estimateJob struct {
	userID  int64
	mealID  int64
	photoID int64
	relPath string
	hint    string
}

// FoodEstimator prices food photos off the request path. A vision call takes
// most of a minute and an upload must not wait for it.
type FoodEstimator struct {
	s       *Server
	workers int
	queue   chan estimateJob
	wg      sync.WaitGroup
	once    sync.Once
}

// NewFoodEstimator builds an estimator with the given worker count and queue.
func NewFoodEstimator(s *Server, workers, depth int) *FoodEstimator {
	return &FoodEstimator{s: s, workers: workers, queue: make(chan estimateJob, depth)}
}

// Start launches the workers and requeues anything left pending by a restart.
func (e *FoodEstimator) Start(ctx context.Context) {
	for i := 0; i < e.workers; i++ {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-e.queue:
					if !ok {
						return
					}
					e.run(ctx, job)
				}
			}
		}()
	}
	go e.requeuePending(ctx)
}

// Stop closes the queue and waits for in-flight work.
func (e *FoodEstimator) Stop() {
	e.once.Do(func() { close(e.queue) })
	e.wg.Wait()
}

// Enqueue schedules a job, marking the meal failed if the queue is full.
func (e *FoodEstimator) Enqueue(job estimateJob) {
	select {
	case e.queue <- job:
	default:
		e.s.log.Warn("estimate queue full", zap.Int64("meal", job.mealID))
		_, _ = e.s.db.Exec(`UPDATE meals SET estimate_status = 'failed', estimate_error = 'estimator busy; retry later' WHERE id = ?`, job.mealID)
	}
}

func (e *FoodEstimator) requeuePending(ctx context.Context) {
	rows, err := e.s.db.QueryContext(ctx, `
		SELECT m.id, m.user_id, m.photo_id, p.rel_path, m.notes
		  FROM meals m JOIN photos p ON p.id = m.photo_id
		 WHERE m.estimate_status = 'pending'`)
	if err != nil {
		return
	}
	var jobs []estimateJob
	for rows.Next() {
		var j estimateJob
		if err := rows.Scan(&j.mealID, &j.userID, &j.photoID, &j.relPath, &j.hint); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	for _, j := range jobs {
		e.Enqueue(j)
	}
}

func (e *FoodEstimator) run(ctx context.Context, job estimateJob) {
	s := e.s
	fail := func(msg string) {
		_, _ = s.db.Exec(`UPDATE meals SET estimate_status = 'failed', estimate_error = ? WHERE id = ?`, msg, job.mealID)
	}
	if err := s.checkAIQuota(ctx, job.userID); err != nil {
		fail(err.Error())
		return
	}
	f, err := s.photos.Open(job.relPath)
	if err != nil {
		fail("photo file missing")
		return
	}
	data, err := readAll(f)
	f.Close()
	if err != nil {
		fail("could not read photo")
		return
	}

	// An identical photo and hint reuse the stored answer.
	if cached, ok := s.cachedResult(ctx, job.userID, aifeatures.FeatureFoodPhoto, hashFor(data, job.hint)); ok {
		var est aifeatures.FoodEstimate
		if decodeCached(cached, &est) == nil && est.Kcal > 0 {
			_ = s.applyEstimate(ctx, job.mealID, &est)
			return
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	est, meta, err := s.ai.EstimateFood(callCtx, data, "image/jpeg", job.hint)
	s.recordAIRun(ctx, job.userID, aifeatures.FeatureFoodPhoto, meta, err)
	if err != nil {
		msg := "estimate failed"
		if errors.Is(err, aifeatures.ErrNoEstimate) {
			msg = strings.TrimPrefix(err.Error(), "aifeatures: ")
		}
		s.log.Warn("food estimate failed", zap.Int64("meal", job.mealID), zap.Error(err))
		fail(msg)
		return
	}
	if err := s.applyEstimate(ctx, job.mealID, est); err != nil {
		s.log.Error("apply estimate", zap.Error(err))
		fail("could not save estimate")
	}
}

// applyEstimate writes an estimate onto a meal, replacing any items.
func (s *Server) applyEstimate(ctx context.Context, mealID int64, est *aifeatures.FoodEstimate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	name := est.Name
	if name == "" {
		name = "Meal"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE meals SET name = CASE WHEN name = '' THEN ? ELSE name END,
		       kcal = ?, protein_g = ?, carbs_g = ?, fat_g = ?, source = 'ai',
		       estimate_status = 'done', estimate_error = '', updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`, name, est.Kcal, est.ProteinG, est.CarbsG, est.FatG, mealID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM meal_items WHERE meal_id = ?`, mealID); err != nil {
		return err
	}
	for i, it := range est.Items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO meal_items (meal_id, name, qty, unit, kcal, protein_g, carbs_g, fat_g, confidence, sort_order)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			mealID, it.Name, it.Qty, it.Unit, it.Kcal, it.ProteinG, it.CarbsG, it.FatG, it.Confidence, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ErrAIQuota is returned when the caller has used the day's allowance.
var ErrAIQuota = fmt.Errorf("daily AI limit reached; it resets over the next 24 hours")
