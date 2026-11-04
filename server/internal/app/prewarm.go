package app

import (
	"time"

	"github.com/ovh-buy/server/internal/types"
)

// OrderPrewarmer prepares carts without placing orders or taking payment.
type OrderPrewarmer interface {
	Prepare(owner string, items []types.QueueItem, lease time.Duration, active func() bool) error
	Ready(item types.QueueItem) bool
	Cancel(owner string)
	CancelPrefix(prefix string)
	Status(owner string) []PrewarmStatus
	Close()
}

type PrewarmStatus struct {
	Duration    string   `json:"duration"`
	ID          string   `json:"id"`
	AccountID   string   `json:"accountId"`
	PlanCode    string   `json:"planCode"`
	Datacenters []string `json:"datacenters"`
	Ready       int      `json:"ready"`
	Total       int      `json:"total"`
	State       string   `json:"state"`
	ExpiresAt   string   `json:"expiresAt,omitempty"`
	Error       string   `json:"error,omitempty"`
	Standby     bool     `json:"standby,omitempty"`
	Estimated   bool     `json:"estimated,omitempty"`
}
