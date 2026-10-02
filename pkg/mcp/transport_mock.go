package mcp

import (
	"context"
	"errors"
	"sync"
)

type MockMCPTransport struct {
	mu        sync.Mutex
	SendFunc  func(ctx context.Context, message []byte) error
	ReceiveFunc func(ctx context.Context) ([]byte, error)
	CloseFunc func() error
	SentMessages [][]byte
	ReceiveCalls int
	CloseCalls int
}

func (m *MockMCPTransport) Send(ctx context.Context, message []byte) error {
	m.mu.Lock()
	m.SentMessages = append(m.SentMessages, message)
	fn := m.SendFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, message)
	}
	return errors.New("Send not implemented")
}

func (m *MockMCPTransport) Receive(ctx context.Context) ([]byte, error) {
	m.mu.Lock()
	m.ReceiveCalls++
	fn := m.ReceiveFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil, errors.New("Receive not implemented")
}

func (m *MockMCPTransport) Close() error {
	m.mu.Lock()
	m.CloseCalls++
	fn := m.CloseFunc
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return errors.New("Close not implemented")
}

func (m *MockMCPTransport) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SentMessages = nil
	m.ReceiveCalls = 0
	m.CloseCalls = 0
}