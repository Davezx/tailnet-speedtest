package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// whoisIdentity resolves a tailnet IP to "machine (user)" via the local
// tailscaled. Returns "" when tailscale is unavailable or the IP is not a peer.
func whoisIdentity(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tailscale", "whois", "--json", ip).Output()
	if err != nil {
		return ""
	}
	var r struct {
		Node struct {
			Name        string
			MachineName string
		}
		UserProfile struct {
			LoginName   string
			DisplayName string
		}
	}
	if json.Unmarshal(out, &r) != nil {
		return ""
	}
	machine := r.Node.MachineName
	if machine == "" {
		machine = r.Node.Name
	}
	user := r.UserProfile.DisplayName
	if user == "" {
		user = r.UserProfile.LoginName
	}
	switch {
	case machine != "" && user != "":
		return machine + " (" + user + ")"
	case machine != "":
		return machine
	default:
		return user
	}
}

// lookupTailscalePeer reports how a peer's traffic reaches this server
// (direct path vs DERP relay). Returns nil when unknown.
func lookupTailscalePeer(ip string) *tailscaleInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var status struct {
		Peer map[string]struct {
			HostName     string
			DNSName      string
			TailscaleIPs []string
			CurAddr      string // non-empty => direct path
			Relay        string
		}
	}
	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil || json.Unmarshal(out, &status) != nil {
		return nil
	}
	for _, p := range status.Peer {
		for _, pIP := range p.TailscaleIPs {
			if pIP != ip {
				continue
			}
			direct := p.CurAddr != ""
			name := p.DNSName
			if name == "" {
				name = p.HostName
			}
			return &tailscaleInfo{PeerName: name, Direct: &direct, Relay: p.Relay}
		}
	}
	return nil
}
