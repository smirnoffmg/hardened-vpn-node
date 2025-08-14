# VPN Xray Node

Production-hardened Docker container for Xray-core VPN proxy with VLESS/XTLS protocol.

## Quick Start

```bash
make run          # Build and start
make health       # Check status
make stop         # Stop container
make help         # Show all commands
```

## API

Health check endpoint:

```bash
curl http://localhost:8080/
```

```json
{
  "success": true,
  "message": "ok",
  "data": {
    "status": "healthy",
    "timestamp": "2024-01-01T12:00:00Z"
  }
}
```

## Configuration

| Variable            | Default                                | Description             |
| ------------------- | -------------------------------------- | ----------------------- |
| `HEALTH_ADDR`       | `:8080`                                | Health endpoint address |
| `XRAY_CLIENT_UUID`  | `00000000-0000-0000-0000-000000000000` | Client UUID             |
| `XRAY_CLIENT_EMAIL` | `default@example.com`                  | Client email            |

```bash
# Custom configuration
docker run -e HEALTH_ADDR=:9090 -e XRAY_CLIENT_UUID=your-uuid xray-node:latest
```

## Security

- **Ports**: `443` (proxy), `8080` (health)
- **Container**: Distroless base, non-root user (65532), read-only filesystem
- **Network**: Rate limiting (10 req/min), security headers, Slowloris protection
- **Profiles**: Seccomp + AppArmor restrictions, dropped capabilities
- **Certificates**: `make certs` for self-signed
- **Scanning**: gosec static analysis, Trivy vulnerability scanning

### Docker Hardening

- **Base**: `gcr.io/distroless/static:nonroot` - minimal attack surface
- **User**: Runs as `65532:65532` (nonroot) instead of root
- **Filesystem**: Read-only with `--read-only` flag
- **Capabilities**: Dropped with `--cap-drop=ALL`
- **Security**: Seccomp + AppArmor profiles applied
- **Temporary**: `/tmp` mounted as tmpfs for writable areas

## License

MIT License
