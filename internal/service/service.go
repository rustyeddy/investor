package service

import (
	"context"

	"github.com/rustyeddy/investor/internal/firm"
)

// Service is the transport-neutral application boundary. It coordinates
// Investor workflows, while CLI/REST/(future MCP) adapt inputs and results.
type Service struct {
	config firm.Config
	engine *firm.Engine
}

func New(c firm.Config, engine *firm.Engine) *Service {
	return &Service{config:c,engine:engine}
}

func (s *Service) Plan(workflow string) ([]firm.Agent,error) {
	return s.engine.Plan(workflow)
}

func (s *Service) Run(ctx context.Context, workflow, objective string) (firm.Run,error) {
	return s.engine.Run(ctx,workflow,objective)
}

func (s *Service) Config() firm.Config { return s.config }
