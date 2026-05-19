package catalog

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

type MemoryStore struct {
	mu          sync.RWMutex
	services    map[string]models.Service
	deployments map[string][]models.Deployment
	workflows   map[string]models.WorkflowRun
	incidents   map[string]models.IncidentRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		services:    make(map[string]models.Service),
		deployments: make(map[string][]models.Deployment),
		workflows:   make(map[string]models.WorkflowRun),
		incidents:   make(map[string]models.IncidentRecord),
	}
}

func (s *MemoryStore) CreateService(_ context.Context, service models.Service) (models.Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.services[service.Name]; exists {
		return models.Service{}, fmt.Errorf("%w: service %q already exists", ErrConflict, service.Name)
	}
	if service.ID == "" {
		service.ID = NewID()
	}
	now := nowUTC()
	service.CreatedAt = now
	service.UpdatedAt = now
	s.services[service.Name] = service
	return service, nil
}

func (s *MemoryStore) ListServices(_ context.Context) ([]models.Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	services := make([]models.Service, 0, len(s.services))
	for _, service := range s.services {
		services = append(services, service)
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})
	return services, nil
}

func (s *MemoryStore) GetServiceByName(_ context.Context, name string) (models.Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	service, ok := s.services[name]
	if !ok {
		return models.Service{}, ErrNotFound
	}
	return service, nil
}

func (s *MemoryStore) RecordDeployment(_ context.Context, deployment models.Deployment) (models.Deployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if deployment.ID == "" {
		deployment.ID = NewID()
	}
	if deployment.StartedAt.IsZero() {
		deployment.StartedAt = nowUTC()
	}
	s.deployments[deployment.ServiceID] = append(s.deployments[deployment.ServiceID], deployment)
	return deployment, nil
}

func (s *MemoryStore) LatestDeployment(_ context.Context, serviceID string) (models.Deployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deployments := s.deployments[serviceID]
	if len(deployments) == 0 {
		return models.Deployment{}, ErrNotFound
	}
	return deployments[len(deployments)-1], nil
}

func (s *MemoryStore) ListDeployments(_ context.Context, serviceID string) ([]models.Deployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deployments := append([]models.Deployment(nil), s.deployments[serviceID]...)
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].StartedAt.After(deployments[j].StartedAt)
	})
	return deployments, nil
}

func (s *MemoryStore) SaveWorkflowRun(_ context.Context, run models.WorkflowRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflows[run.ID] = run
	return nil
}

func (s *MemoryStore) GetWorkflowRun(_ context.Context, id string) (models.WorkflowRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.workflows[id]
	if !ok {
		return models.WorkflowRun{}, ErrNotFound
	}
	return run, nil
}

func (s *MemoryStore) GetWorkflowRunByIdempotencyKey(_ context.Context, key string) (models.WorkflowRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, run := range s.workflows {
		if run.IdempotencyKey == key {
			return run, nil
		}
	}
	return models.WorkflowRun{}, ErrNotFound
}

func (s *MemoryStore) ListWorkflowRuns(_ context.Context) ([]models.WorkflowRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]models.WorkflowRun, 0, len(s.workflows))
	for _, run := range s.workflows {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	return runs, nil
}

func (s *MemoryStore) SaveIncidentRecord(_ context.Context, record models.IncidentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.incidents[record.Incident.ID] = record
	return nil
}

func (s *MemoryStore) GetIncidentRecord(_ context.Context, id string) (models.IncidentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.incidents[id]
	if !ok {
		return models.IncidentRecord{}, ErrNotFound
	}
	return record, nil
}

func (s *MemoryStore) ListIncidentRecords(_ context.Context) ([]models.IncidentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]models.IncidentRecord, 0, len(s.incidents))
	for _, record := range s.incidents {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Incident.CreatedAt.After(records[j].Incident.CreatedAt)
	})
	return records, nil
}
