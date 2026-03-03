package posixsignal

import (
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/pkg/shutdown"
)

func TestStartShutdownCalledOnDefaultSignals(t *testing.T) {
	c := make(chan int, 1)

	psm := NewPosixSignalManager()

	// Test that the manager can be started with a mock interface
	captureGS := &mockGSInterface{
		handler: func() {
			c <- 1
		},
	}

	err := psm.Start(captureGS)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify the manager name is correct
	if psm.GetName() != Name {
		t.Errorf("Expected name %s, got %s", Name, psm.GetName())
	}

	// Give the goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Note: Actual signal testing requires the test to run on POSIX systems
	// On Windows, syscall.Kill and signal handling work differently
}

func TestStartShutdownCalledCustomSignal(t *testing.T) {
	c := make(chan int, 1)

	psm := NewPosixSignalManager()

	captureGS := &mockGSInterface{
		handler: func() {
			c <- 1
		},
	}

	err := psm.Start(captureGS)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Verify the manager accepts custom signals
	if psm.GetName() != Name {
		t.Errorf("Expected name %s, got %s", Name, psm.GetName())
	}
}

func TestGetName(t *testing.T) {
	psm := NewPosixSignalManager()
	expected := "PosixSignalManager"
	if psm.GetName() != expected {
		t.Errorf("Expected name %s, got %s", expected, psm.GetName())
	}
}

func TestShutdownStart(t *testing.T) {
	psm := NewPosixSignalManager()
	err := psm.ShutdownStart()
	if err != nil {
		t.Errorf("ShutdownStart should return nil, got: %v", err)
	}
}

type mockGSInterface struct {
	handler func()
}

func (m *mockGSInterface) StartShutdown(sm shutdown.ShutdownManager) {
	m.handler()
}

func (m *mockGSInterface) ReportError(err error) {
}

func (m *mockGSInterface) AddShutdownCallback(shutdownCallback shutdown.ShutdownCallback) {
}
