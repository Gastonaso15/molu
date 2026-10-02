package xolu

import (
	"context"
)

type WalkRequest struct {
	Tool      string
	Input     map[string]any
	TenantID  string
	Context   map[string]any
}

type WalkResponse struct {
	Output    map[string]any
	NextState string
}

type FindRequest struct {
	Query     map[string]any
	TenantID  string
	Limit     int
	Offset    int
}

type FindResponse struct {
	Results []map[string]any
	Total   int
}

type GetRequest struct {
	ID       string
	TenantID string
}

type GetResponse struct {
	Object map[string]any
}

type XoluClient interface {
	Walk(ctx context.Context, req WalkRequest) (WalkResponse, error)
	Find(ctx context.Context, req FindRequest) (FindResponse, error)
	Get(ctx context.Context, req GetRequest) (GetResponse, error)
	Ping(ctx context.Context) error
}