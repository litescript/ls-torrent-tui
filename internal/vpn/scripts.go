// Package vpn provides VPN status checking and connection management.
// Currently implements NordVPN support via external scripts, with plans
// for native Go implementation.
//
// This file (scripts.go) contains the legacy script-based implementation.
// See native.go for the future native NordVPN implementation.
package vpn

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Status represents VPN connection state
type Status struct {
	Connected bool
	Server    string
	Country   string
	IP        string
	Error     error
}

// Checker polls VPN status
type Checker struct {
	statusScript  string
	connectScript string
}

// NewChecker creates a VPN status checker
func NewChecker(statusScript, connectScript string) *Checker {
	return &Checker{
		statusScript:  statusScript,
		connectScript: connectScript,
	}
}

// Check runs the status script and parses output
func (c *Checker) Check(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", c.statusScript)
	output, err := cmd.Output()
	if err != nil {
		// Script failed or VPN not connected
		return Status{Connected: false, Error: err}
	}

	return parseStatus(string(output))
}

// Connect runs the connect script
func (c *Checker) Connect(ctx context.Context) error {
	// 60s timeout - script checks 25 servers for lowest latency
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", c.connectScript)
	return cmd.Run()
}

// parseStatus extracts VPN info from nordvpn status output
func parseStatus(output string) Status {
	s := Status{}
	lower := strings.ToLower(output)

	// Check for connected indicators - multiple formats:
	// - "connected" (standard nordvpn)
	// - "interface: UP" (tun0 check)
	// - "Status: Connected"
	if strings.Contains(lower, "interface: up") ||
		strings.Contains(lower, "tun0") && strings.Contains(lower, "up") ||
		(strings.Contains(lower, "connected") && !strings.Contains(lower, "disconnected")) {
		s.Connected = true
	}

	// Parse output line by line
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		lineLower := strings.ToLower(line)

		if strings.HasPrefix(lineLower, "server:") {
			s.Server = strings.TrimSpace(line[7:])
		} else if strings.HasPrefix(lineLower, "country:") {
			s.Country = strings.TrimSpace(line[8:])
		} else if strings.HasPrefix(lineLower, "netname:") {
			// NORDVPN-L1 style
			s.Server = strings.TrimSpace(line[8:])
		} else if strings.HasPrefix(lineLower, "public ip:") {
			s.IP = strings.TrimSpace(line[10:])
		} else if strings.HasPrefix(lineLower, "ip:") {
			s.IP = strings.TrimSpace(line[3:])
		} else if strings.HasPrefix(lineLower, "your new ip:") {
			s.IP = strings.TrimSpace(line[12:])
		}
	}

	return s
}

// StatusString returns a short status string for display
func (s Status) StatusString() string {
	if s.Connected {
		if s.Country != "" {
			return "VPN: " + s.Country
		}
		if s.Server != "" {
			return "VPN: " + s.Server
		}
		return "VPN: Connected"
	}
	return "VPN: Disconnected"
}
