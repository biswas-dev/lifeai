package api

import (
	ai "github.com/anchoo2kewl/go-ai"

	"github.com/biswas-dev/lifeai/api/internal/aifeatures"
)

func (s *Server) aiChain() *ai.Chain { return s.ai.Chain() }

func aiMetaFrom(resp ai.Response, hash string) aifeatures.Meta {
	return aifeatures.MetaFrom(resp, hash)
}
