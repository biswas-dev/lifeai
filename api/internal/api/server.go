// Package api holds the HTTP server: middleware, handlers and the types they
// exchange with the SPA.
package api

import (
	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/aifeatures"
	"github.com/biswas-dev/lifeai/api/internal/config"
	"github.com/biswas-dev/lifeai/api/internal/db"
	"github.com/biswas-dev/lifeai/api/internal/photo"
	"github.com/biswas-dev/lifeai/api/internal/secret"
)

// Server carries the dependencies every handler needs.
type Server struct {
	db     *db.DB
	cfg    *config.Config
	log    *zap.Logger
	photos *photo.Store
	ai     *aifeatures.Service
	cipher *secret.Cipher
	// food, when set, estimates food photos off the request path. Nil is a
	// valid state: without it a food photo is simply saved without numbers.
	food *FoodEstimator
	// syncer, when set, runs the 75hard pull. Nil means the bridge is off.
	syncer *Hard75Syncer
}

// NewServer builds a Server.
func NewServer(database *db.DB, cfg *config.Config, log *zap.Logger, photos *photo.Store, aiSvc *aifeatures.Service, cipher *secret.Cipher) *Server {
	SetLogger(log)
	return &Server{db: database, cfg: cfg, log: log, photos: photos, ai: aiSvc, cipher: cipher}
}

// SetFoodEstimator attaches the background estimator.
func (s *Server) SetFoodEstimator(e *FoodEstimator) { s.food = e }

// SetHard75Syncer attaches the 75hard puller.
func (s *Server) SetHard75Syncer(h *Hard75Syncer) { s.syncer = h }

// DB exposes the database for the admin CLI and tests.
func (s *Server) DB() *db.DB { return s.db }

// Logger exposes the logger for the background workers.
func (s *Server) Logger() *zap.Logger { return s.log }
