package xolu

import (
	"context"
	"errors"
	"sync"
)

type MockXoluClient struct {
	mu           sync.Mutex
	WalkFunc     func(ctx context.Context, req WalkRequest) (WalkResponse, error)
	FindFunc     func(ctx context.Context, req FindRequest) (FindResponse, error)
	GetFunc      func(ctx context.Context, req GetRequest) (GetResponse, error)
	PingFunc     func(ctx context.Context) error
	WalkCalls    []WalkRequest
	FindCalls    []FindRequest
	GetCalls     []GetRequest
	PingCalls    int
}

func (m *MockXoluClient) Walk(ctx context.Context, req WalkRequest) (WalkResponse, error) {
	m.mu.Lock()
	m.WalkCalls = append(m.WalkCalls, req)
	fn := m.WalkFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return WalkResponse{}, errors.New("Walk not implemented")
}

func (m *MockXoluClient) Find(ctx context.Context, req FindRequest) (FindResponse, error) {
	m.mu.Lock()
	m.FindCalls = append(m.FindCalls, req)
	fn := m.FindFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return FindResponse{}, errors.New("Find not implemented")
}

func (m *MockXoluClient) Get(ctx context.Context, req GetRequest) (GetResponse, error) {
	m.mu.Lock()
	m.GetCalls = append(m.GetCalls, req)
	fn := m.GetFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return GetResponse{}, errors.New("Get not implemented")
}

func (m *MockXoluClient) Ping(ctx context.Context) error {
	m.mu.Lock()
	m.PingCalls++
	fn := m.PingFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return errors.New("Ping not implemented")
}

func (m *MockXoluClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WalkCalls = nil
	m.FindCalls = nil
	m.GetCalls = nil
	m.PingCalls = 0
}