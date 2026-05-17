package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

type Store interface {
	CreateService(context.Context, models.Service) (models.Service, error)
	ListServices(context.Context) ([]models.Service, error)
	GetServiceByName(context.Context, string) (models.Service, error)
	RecordDeployment(context.Context, models.Deployment) (models.Deployment, error)
	LatestDeployment(context.Context, string) (models.Deployment, error)
	ListDeployments(context.Context, string) ([]models.Deployment, error)
	SaveWorkflowRun(context.Context, models.WorkflowRun) error
	GetWorkflowRun(context.Context, string) (models.WorkflowRun, error)
	ListWorkflowRuns(context.Context) ([]models.WorkflowRun, error)
}

func Open(ctx context.Context, databaseURL string) (Store, func(), error) {
	if databaseURL == "" {
		return NewMemoryStore(), func() {}, nil
	}

	store, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		return nil, nil, err
	}
	return store, func() { _ = store.Close() }, nil
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("read random bytes: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

func nowUTC() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func nullableTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
