package signals

import (
	"context"
	"io"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestSetupCancellableContextCreatesValidContext verifies that the function
// returns a valid context and cleanup function
func TestSetupCancellableContextCreatesValidContext(t *testing.T) {
	ctx, cleanup := SetupCancellableContext()

	if ctx == nil {
		t.Fatal("Context should not be nil")
	}

	if cleanup == nil {
		t.Fatal("Cleanup function should not be nil")
	}

	// Context should not be cancelled initially
	select {
	case <-ctx.Done():
		t.Fatal("Context should not be cancelled initially")
	default:
		// Expected: context is not cancelled
	}

	// Cleanup should not block
	cleanup()
}

// TestCleanupDoesNotHang verifies that calling cleanup() completes quickly
// and doesn't cause the process to hang
func TestCleanupDoesNotHang(t *testing.T) {
	ctx, cleanup := SetupCancellableContext()

	if ctx == nil {
		t.Fatal("Context should not be nil")
	}

	// Call cleanup in a goroutine with timeout
	done := make(chan struct{})
	go func() {
		cleanup()
		close(done)
	}()

	// Wait for cleanup with timeout
	select {
	case <-done:
		// Success: cleanup completed
	case <-time.After(2 * time.Second):
		t.Fatal("Cleanup hung and did not complete within 2 seconds")
	}
}

// TestCleanupCancelsContext verifies that cleanup cancels the context
func TestCleanupCancelsContext(t *testing.T) {
	ctx, cleanup := SetupCancellableContext()

	// Context should not be cancelled initially
	select {
	case <-ctx.Done():
		t.Fatal("Context should not be cancelled initially")
	default:
		// Expected
	}

	// Call cleanup
	cleanup()

	// Context should now be cancelled
	select {
	case <-ctx.Done():
		// Expected: context is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled after cleanup")
	}

	// Verify context error
	if ctx.Err() != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", ctx.Err())
	}
}

// TestNoGoroutineLeak verifies that the signal handler goroutine exits
// after cleanup is called
func TestNoGoroutineLeak(t *testing.T) {
	// Get initial goroutine count
	initialCount := runtime.NumGoroutine()

	// Create context and cleanup immediately
	ctx, cleanup := SetupCancellableContext()
	_ = ctx

	cleanup()

	// Give goroutines time to exit
	time.Sleep(100 * time.Millisecond)

	// Check goroutine count
	finalCount := runtime.NumGoroutine()

	// Final count should be same or less than initial
	// (we allow "same" because other test goroutines might be running)
	if finalCount > initialCount+1 {
		t.Errorf("Goroutine leak detected: initial=%d, final=%d", initialCount, finalCount)
	}
}

// TestMultipleCleanupCallsAreSafe verifies that calling cleanup multiple times
// doesn't cause panics or hangs
func TestMultipleCleanupCallsAreSafe(t *testing.T) {
	ctx, cleanup := SetupCancellableContext()
	_ = ctx

	// First cleanup
	cleanup()

	// Second cleanup should not panic or hang
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Cleanup panicked on second call: %v", r)
			}
			close(done)
		}()
		cleanup()
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Second cleanup call hung")
	}
}

// TestSignalDeliveryDoesNotCrash verifies that sending a signal after setup
// doesn't cause crashes (though we can't easily verify cancellation in tests)
func TestSignalDeliveryDoesNotCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping signal delivery test in short mode")
	}

	ctx, cleanup := SetupCancellableContext()
	defer cleanup()

	// Send interrupt signal to ourselves
	// Note: This will be caught by the signal handler
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("Failed to find current process: %v", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Failed to send signal: %v", err)
	}

	// Wait a bit for signal to be processed
	time.Sleep(100 * time.Millisecond)

	// Context should be cancelled by the signal handler
	select {
	case <-ctx.Done():
		// Expected: signal handler cancelled the context
		t.Log("Context was cancelled by signal handler (expected)")
	default:
		// Note: On some systems, SIGTERM might not be delivered in tests
		t.Log("Context was not cancelled - signal might not have been delivered in test environment")
	}

	// Cleanup should still work even after signal
	cleanup()
}

// TestContextPropagation verifies that the context can be used with
// context-aware operations
func TestContextPropagation(t *testing.T) {
	ctx, cleanup := SetupCancellableContext()
	defer cleanup()

	// Create a child context
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	// Child should not be cancelled initially
	select {
	case <-childCtx.Done():
		t.Fatal("Child context should not be cancelled initially")
	default:
		// Expected
	}

	// Call cleanup to cancel parent
	cleanup()

	// Child should also be cancelled when parent is cancelled
	select {
	case <-childCtx.Done():
		// Expected: child inherits parent cancellation
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Child context should be cancelled when parent is cancelled")
	}
}

// BenchmarkSetupCancellableContext measures the performance overhead
// of setting up the cancellable context
func BenchmarkSetupCancellableContext(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx, cleanup := SetupCancellableContext()
		_ = ctx
		cleanup()
	}
}

// --- SetupSilentCancellableContext ---
//
// The silent variant shares its implementation with SetupCancellableContext;
// these tests pin the behavior that must stay identical (cancellation,
// goroutine lifecycle, idempotent cleanup) plus the one behavior that must
// differ: it prints nothing when a signal arrives.

// TestSetupSilentCancellableContextCreatesValidContext verifies the silent
// variant returns a usable, not-yet-cancelled context and a cleanup function.
func TestSetupSilentCancellableContextCreatesValidContext(t *testing.T) {
	ctx, cleanup := SetupSilentCancellableContext()

	if ctx == nil {
		t.Fatal("Context should not be nil")
	}
	if cleanup == nil {
		t.Fatal("Cleanup function should not be nil")
	}

	select {
	case <-ctx.Done():
		t.Fatal("Context should not be cancelled initially")
	default:
		// Expected: context is not cancelled
	}

	cleanup()
}

// TestSilentCleanupCancelsContext verifies cleanup cancels the silent context.
func TestSilentCleanupCancelsContext(t *testing.T) {
	ctx, cleanup := SetupSilentCancellableContext()

	cleanup()

	select {
	case <-ctx.Done():
		// Expected: context is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled after cleanup")
	}

	if ctx.Err() != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", ctx.Err())
	}
}

// TestSilentNoGoroutineLeak verifies the silent handler goroutine exits after
// cleanup is called.
func TestSilentNoGoroutineLeak(t *testing.T) {
	initialCount := runtime.NumGoroutine()

	_, cleanup := SetupSilentCancellableContext()
	cleanup()

	// Give the goroutine time to exit.
	time.Sleep(100 * time.Millisecond)

	finalCount := runtime.NumGoroutine()
	if finalCount > initialCount+1 {
		t.Errorf("Goroutine leak detected: initial=%d, final=%d", initialCount, finalCount)
	}
}

// TestSilentMultipleCleanupCallsAreSafe verifies that calling the silent
// variant's cleanup multiple times doesn't panic or hang.
func TestSilentMultipleCleanupCallsAreSafe(t *testing.T) {
	_, cleanup := SetupSilentCancellableContext()

	cleanup()

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Cleanup panicked on second call: %v", r)
			}
			close(done)
		}()
		cleanup()
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Second cleanup call hung")
	}
}

// TestSilentSignalDeliveryCancelsWithoutPrinting verifies that a signal cancels
// the silent context while writing nothing to stdout — the whole reason the
// silent variant exists (any print would garble a live TUI). The stdout
// assertion holds whether or not the signal is delivered in this environment:
// the silent handler never prints, so captured output must always be empty.
func TestSilentSignalDeliveryCancelsWithoutPrinting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping signal delivery test in short mode")
	}

	// Redirect stdout so we can prove the handler stays quiet. fmt.Println
	// reads the os.Stdout package variable at call time, so swapping it here
	// captures anything the handler would otherwise print.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	ctx, cleanup := SetupSilentCancellableContext()
	defer cleanup()

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("Failed to find current process: %v", err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Failed to send signal: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Log("Context was cancelled by signal handler (expected)")
	case <-time.After(500 * time.Millisecond):
		t.Log("Context was not cancelled - signal might not have been delivered in test environment")
	}

	// cleanup() stops signal delivery and waits for the handler goroutine to
	// exit, so no write can race the read below. Restore stdout and close the
	// writer so ReadAll sees EOF.
	cleanup()
	os.Stdout = origStdout
	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close pipe writer: %v", err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Failed to read captured stdout: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("Silent handler wrote to stdout: %q", out)
	}
}
