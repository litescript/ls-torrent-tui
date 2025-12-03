// native.go contains stubs for future native NordVPN implementation.
// This will replace the script-based approach in scripts.go.
package vpn

import (
	"context"
	"errors"
)

// ErrNotImplemented is returned by native VPN operations that are not yet implemented.
var ErrNotImplemented = errors.New("native VPN support not yet implemented")

// NativeChecker provides native NordVPN integration without external scripts.
// TODO: Implement using one of:
//   - NordVPN CLI parsing (nordvpn status)
//   - NordVPN Linux daemon socket
//   - NordVPN API (requires authentication)
type NativeChecker struct {
	// TODO: Add fields for:
	// - preferred server/country
	// - connection preferences
	// - daemon socket path
}

// NewNativeChecker creates a native VPN checker.
// TODO: Implement configuration options.
func NewNativeChecker() *NativeChecker {
	return &NativeChecker{}
}

// Check returns the current VPN connection status.
// TODO: Implement native status checking.
func (c *NativeChecker) Check(ctx context.Context) Status {
	return Status{
		Connected: false,
		Error:     ErrNotImplemented,
	}
}

// Connect establishes a VPN connection.
// TODO: Implement native connection logic with:
//   - Server selection (fastest, specific country, specific server)
//   - Retry logic with backoff
//   - Connection state tracking
func (c *NativeChecker) Connect(ctx context.Context) error {
	return ErrNotImplemented
}

// Disconnect terminates the VPN connection.
// TODO: Implement native disconnection.
func (c *NativeChecker) Disconnect(ctx context.Context) error {
	return ErrNotImplemented
}

// ListServers returns available NordVPN servers.
// TODO: Implement server listing with filtering by:
//   - Country
//   - Server type (standard, P2P, obfuscated)
//   - Load percentage
func (c *NativeChecker) ListServers(ctx context.Context) ([]string, error) {
	return nil, ErrNotImplemented
}
