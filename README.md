# VPN Xray Node

Production-hardened Docker container for Xray-core VPN proxy with VLESS/XTLS protocol.

## Quick Start

```bash
# Build and run
make run

# Check status
make status

# View logs
make logs
```

## Features

- **VLESS/XTLS** - Modern proxy protocol with TLS encryption
- **Security Hardened** - Distroless base, non-root, read-only filesystem
- **Health Monitoring** - Built-in health endpoint on port 8080
- **Certificate Support** - Self-signed, Let's Encrypt, or REALITY

## Commands

```bash
make build     # Build Docker image
make run       # Start container (development)
make run-prod  # Start with full security profiles
make stop      # Stop container
make logs      # View logs
make status    # Check status
make health    # Health check
make certs     # Generate self-signed certificates
make clean     # Remove everything
```

## API Endpoints

The VPN node provides a REST API for management:

### Health & System
- `GET /` - Health check with system status
- `GET /api/system` - System information and statistics
- `POST /api/reload` - Reload configuration

### User Management
- `GET /api/users` - List all users
- `POST /api/users` - Create new user
- `GET /api/users/{id}` - Get user details
- `PUT /api/users/{id}` - Update user
- `DELETE /api/users/{id}` - Delete user

### Traffic Statistics
- `GET /api/stats` - List all user statistics
- `GET /api/stats/{id}` - Get user statistics

### Quota Management
- `GET /api/quotas` - List all quotas
- `POST /api/quotas` - Create quota
- `GET /api/quotas/{id}` - Get quota details
- `PUT /api/quotas/{id}` - Update quota

### Example Usage

```bash
# Check health
curl http://localhost:8080/

# List users
curl http://localhost:8080/api/users

# Create user
curl -X POST http://localhost:8080/api/users \
  -H "Content-Type: application/json" \
  -d '{"id":"user-123","email":"user@example.com","level":0,"flow":"xtls-rprx-vision"}'

# Get system info
curl http://localhost:8080/api/system
```

## Configuration

Edit `config.json` to customize:
- VLESS clients (UUID, flow, email)
- TLS settings and certificates
- Ports and protocols

## Certificate Options

1. **Self-signed** (default): `make certs`
2. **Let's Encrypt**: Copy certificates to `./certs/`
3. **REALITY**: Update config for certificate-less operation

## Security

- Distroless base image
- Non-root execution (uid 65532)
- Read-only filesystem
- Seccomp + AppArmor profiles
- Minimal capabilities

## Ports

- `8443` - VLESS proxy endpoint
- `8080` - Health check endpoint

## Production

For production deployment:
1. Use Let's Encrypt certificates
2. Run with `make run-prod`
3. Configure proper firewall rules
4. Set up monitoring and logging

## License

MIT License
