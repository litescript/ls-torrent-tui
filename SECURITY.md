# Security Policy

## Scope

ls-torrent-tui is a **local terminal application** for managing qBittorrent and organizing files. It does not operate a public service, accept network connections, or store credentials remotely.

The application:
- Runs locally on your machine
- Connects to a local or user-configured qBittorrent instance
- Stores configuration in a local file (`~/.config/torrent-tui/config.toml`)
- Does not transmit data to external services (except user-configured search providers)

## Threat Model

ls-torrent-tui is a **local** terminal app talking to a **local/LAN** qBittorrent instance.
It is not a privacy, anonymity, or VPN tool. Network exposure of qBittorrent is the user's responsibility.

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **Do not** disclose the issue publicly until maintainers have had a chance to respond
2. Open a **private security advisory** via GitHub's Security tab, or
3. Contact the maintainers directly via GitHub Issues with `[SECURITY]` in the title

Please include:
- A description of the vulnerability
- Steps to reproduce the issue
- Potential impact
- Any suggested fixes (optional)

## Response Timeline

We aim to:
- Acknowledge receipt within **48 hours**
- Provide an initial assessment within **7 days**
- Release a fix for confirmed vulnerabilities as soon as practical

## Security Considerations

### Configuration
- qBittorrent credentials are stored in plaintext in the config file
- Protect your config file with appropriate file permissions (`chmod 600`)
- Consider using qBittorrent's built-in authentication features

### Search Providers
- User-configured search providers are treated as untrusted input
- URLs and content from search results are sanitized before display
- Magnet links are passed directly to qBittorrent without modification

### File Operations
- File move operations validate paths to prevent directory traversal
- Operations are restricted to user-configured library paths

## Out of Scope

The following are not considered security vulnerabilities for this project:
- Issues requiring physical access to the machine
- Social engineering attacks
- Vulnerabilities in qBittorrent itself (report those upstream)
- Vulnerabilities in user-configured external scripts
