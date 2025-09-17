package token

import (
	"context"
)

// RefreshManager interface defines the methods needed for token refresh operations
type RefreshManagerInterface interface {
	ScheduleRefresh(ctx context.Context, namespace, name, repositoryURL string) error
	CancelRefresh(namespace, name string)
	Start(ctx context.Context) error
	Stop()
}

// Ensure RefreshManager implements RefreshManagerInterface
var _ RefreshManagerInterface = (*RefreshManager)(nil)
