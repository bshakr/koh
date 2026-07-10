package signals

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// SetupCancellableContext creates a context that can be cancelled by interrupt signals (Ctrl+C, SIGTERM).
// It returns the context and a cleanup function that must be called (typically via defer) to:
// - Stop signal notifications
// - Cancel the context
// - Wait for the signal handler goroutine to exit
//
// On the first signal it prints "Operation cancelled by user" to stdout. Use
// SetupSilentCancellableContext instead when a full-screen TUI owns the
// terminal, where a stray stdout write would garble the display.
//
// Example usage:
//
//	ctx, cleanup := signals.SetupCancellableContext()
//	defer cleanup()
//
//	// Use ctx for long-running operations
//	if err := someOperation(ctx); err != nil {
//	    return err
//	}
func SetupCancellableContext() (context.Context, func()) {
	return setupCancellableContext(true)
}

// SetupSilentCancellableContext behaves exactly like SetupCancellableContext
// but prints nothing when a signal arrives. It is meant for commands that
// drive a bubbletea (or other full-screen) TUI: the TUI owns the terminal, so
// the caller must report cancellation through its own rendering rather than
// letting the signal handler write over the live screen.
func SetupSilentCancellableContext() (context.Context, func()) {
	return setupCancellableContext(false)
}

// setupCancellableContext holds the shared implementation. announce controls
// whether the signal handler prints a cancellation notice; cancellation
// semantics are identical either way.
func setupCancellableContext(announce bool) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	// Setup signal channel
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Signal handler goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-sigChan:
			if announce {
				fmt.Println("\nOperation cancelled by user")
			}
			cancel()
		case <-ctx.Done():
			// Context cancelled or operation completed, exit goroutine
		}
	}()

	// Cleanup function
	cleanup := func() {
		signal.Stop(sigChan) // Stop receiving signals
		cancel()             // Signal goroutine to exit if still running
		<-done               // Wait for signal handler to finish
	}

	return ctx, cleanup
}
