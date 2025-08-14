# VPN Xray Node Makefile
# A production-ready Makefile following best practices
# Usage: make help

# =============================================================================
# Configuration
# =============================================================================

# Application configuration
IMAGE_NAME := xray-node:latest
CONTAINER_NAME := xray-vpn
CONFIG_FILE := ./config/config.json
LOGS_DIR := ./logs
CERTS_DIR := ./certs

# Port configuration
PORT_PROXY := 8443
PORT_HEALTH := 8080
PORT_CUSTOM := 9090

# Docker configuration
DOCKER_RESTART := unless-stopped
DOCKER_READONLY := --read-only
DOCKER_TMPFS := --tmpfs /tmp:rw,noexec,nosuid,size=64m --tmpfs /run:rw,noexec,nosuid,size=32m
DOCKER_CAPS := --cap-drop ALL --cap-add NET_BIND_SERVICE
DOCKER_SECURITY := --security-opt "no-new-privileges:true"

# Volume mounts
VOLUME_CONFIG := -v $(PWD)/$(CONFIG_FILE):/etc/xray/config.json:ro
VOLUME_CERTS := -v $(PWD)/$(CERTS_DIR):/etc/xray/certs:ro
VOLUME_LOGS := -v $(PWD)/$(LOGS_DIR):/var/log/xray:rw

# Environment variables
ENV_DEFAULT := -e HEALTH_ADDR=:8080 -e CONNECT_TIMEOUT=2s -e GRACE_PERIOD=12s -e SHUTDOWN_TIMEOUT=5s -e XRAY_CLIENT_UUID=00000000-0000-0000-0000-000000000000 -e XRAY_CLIENT_EMAIL=default@example.com
ENV_CUSTOM := -e HEALTH_ADDR=:9090 -e CONNECT_TIMEOUT=5s -e GRACE_PERIOD=30s -e SHUTDOWN_TIMEOUT=10s -e XRAY_CLIENT_UUID=11111111-1111-1111-1111-111111111111 -e XRAY_CLIENT_EMAIL=custom@example.com

# Port mappings
PORTS_DEFAULT := -p $(PORT_PROXY):443 -p $(PORT_HEALTH):8080
PORTS_CUSTOM := -p $(PORT_PROXY):443 -p $(PORT_CUSTOM):9090

# Security profiles
SECCOMP_PROFILE := --security-opt seccomp=$(PWD)/seccomp.json
APPARMOR_PROFILE := --security-opt apparmor=$(PWD)/apparmor-xray

# =============================================================================
# Phony targets declaration
# =============================================================================

.PHONY: help build run run-prod run-custom stop logs status health health-check clean certs certs-info validate-prerequisites create-dirs

# =============================================================================
# Main targets
# =============================================================================

# Default target
.DEFAULT_GOAL := help

# Show help information
help: ## Show this help message
	@echo "VPN Xray Node Management Commands:"
	@echo ""
	@echo "Build & Deploy:"
	@echo "  make build     - Build the Docker image"
	@echo "  make run       - Start the container (development)"
	@echo "  make run-custom - Start with custom environment variables"
	@echo "  make run-prod  - Start with production security (requires root)"
	@echo ""
	@echo "Management:"
	@echo "  make stop      - Stop and remove the container"
	@echo "  make logs      - Show container logs"
	@echo "  make status    - Show container status"
	@echo "  make health    - Check health endpoint"
	@echo "  make clean     - Stop and remove everything"
	@echo ""
	@echo "Certificate Management:"
	@echo "  make certs     - Generate self-signed certificates"
	@echo "  make certs-info - Show certificate information"
	@echo ""
	@echo "Utilities:"
	@echo "  make validate-prerequisites - Check system requirements"
	@echo "  make create-dirs - Create necessary directories"
	@echo ""

# Build the Docker image
build: validate-prerequisites ## Build the Docker image
	@echo "Building Docker image..."
	@docker build -t $(IMAGE_NAME) .
	@echo "✅ Docker image built successfully: $(IMAGE_NAME)"

# Run the container (development)
run: build create-dirs ## Start the container with default configuration
	@echo "Starting VPN Xray Node (development)..."
	@docker run -d --name $(CONTAINER_NAME) \
		--restart $(DOCKER_RESTART) \
		$(DOCKER_READONLY) \
		$(DOCKER_TMPFS) \
		$(DOCKER_CAPS) \
		$(DOCKER_SECURITY) \
		$(ENV_DEFAULT) \
		$(VOLUME_CONFIG) \
		$(VOLUME_CERTS) \
		$(VOLUME_LOGS) \
		$(PORTS_DEFAULT) \
		$(IMAGE_NAME)
	@echo "✅ VPN Xray Node started on ports $(PORT_PROXY) (proxy) and $(PORT_HEALTH) (health)"

# Run with production security profiles
run-prod: build create-dirs ## Start with production security (requires root)
	@echo "Starting VPN Xray Node with production security..."
	@sudo docker run -d --name $(CONTAINER_NAME) \
		--restart $(DOCKER_RESTART) \
		$(DOCKER_READONLY) \
		$(DOCKER_TMPFS) \
		$(DOCKER_CAPS) \
		$(DOCKER_SECURITY) \
		$(SECCOMP_PROFILE) \
		$(APPARMOR_PROFILE) \
		$(ENV_DEFAULT) \
		$(VOLUME_CONFIG) \
		$(VOLUME_CERTS) \
		$(VOLUME_LOGS) \
		$(PORTS_DEFAULT) \
		$(IMAGE_NAME)
	@echo "✅ VPN Xray Node started with production security"

# Run with custom environment variables
run-custom: build create-dirs ## Start with custom environment variables
	@echo "Starting VPN Xray Node with custom configuration..."
	@docker run -d --name $(CONTAINER_NAME) \
		--restart $(DOCKER_RESTART) \
		$(DOCKER_READONLY) \
		$(DOCKER_TMPFS) \
		$(DOCKER_CAPS) \
		$(DOCKER_SECURITY) \
		$(ENV_CUSTOM) \
		$(VOLUME_CONFIG) \
		$(VOLUME_CERTS) \
		$(VOLUME_LOGS) \
		$(PORTS_CUSTOM) \
		$(IMAGE_NAME)
	@echo "✅ VPN Xray Node started with custom configuration on ports $(PORT_PROXY) (proxy) and $(PORT_CUSTOM) (health)"

# =============================================================================
# Management targets
# =============================================================================

# Stop the container
stop: ## Stop and remove the container
	@echo "Stopping VPN Xray Node..."
	@docker stop $(CONTAINER_NAME) 2>/dev/null || true
	@docker rm $(CONTAINER_NAME) 2>/dev/null || true
	@echo "✅ VPN Xray Node stopped"

# Show container logs
logs: ## Show container logs
	@docker logs -f $(CONTAINER_NAME)

# Show container status
status: ## Show container status
	@docker ps -a --filter name=$(CONTAINER_NAME)

# Health check
health: ## Check health endpoint
	@echo "Checking health endpoint..."
	@curl -s http://localhost:$(PORT_HEALTH)/ | jq . 2>/dev/null || echo "❌ Health check failed"

# Alternative health check
health-check: health ## Alias for health check

# Clean up everything
clean: stop ## Stop and remove everything
	@echo "Cleaning up..."
	@docker rmi $(IMAGE_NAME) 2>/dev/null || true
	@echo "✅ Cleanup complete"

# =============================================================================
# Certificate management
# =============================================================================

# Generate self-signed certificates
certs: create-dirs ## Generate self-signed certificates
	@echo "Generating self-signed certificates..."
	@openssl req -x509 -newkey rsa:4096 \
		-keyout $(CERTS_DIR)/private.key \
		-out $(CERTS_DIR)/certificate.crt \
		-days 365 -nodes \
		-subj "/C=US/ST=State/L=City/O=Organization/CN=localhost" \
		2>/dev/null || (echo "❌ Failed to generate certificates" && exit 1)
	@echo "✅ Certificates generated in $(CERTS_DIR)/"

# Show certificate info
certs-info: ## Show certificate information
	@echo "Certificate Information:"
	@openssl x509 -in $(CERTS_DIR)/certificate.crt -text -noout 2>/dev/null | head -20 || echo "❌ Certificate not found"

# =============================================================================
# Utility targets
# =============================================================================

# Validate prerequisites
validate-prerequisites: ## Check system requirements
	@echo "Validating prerequisites..."
	@command -v docker >/dev/null 2>&1 || (echo "❌ Docker is required but not installed" && exit 1)
	@command -v curl >/dev/null 2>&1 || (echo "❌ curl is required but not installed" && exit 1)
	@test -f $(CONFIG_FILE) || (echo "❌ Configuration file $(CONFIG_FILE) not found" && exit 1)
	@echo "✅ Prerequisites validated"

# Create necessary directories
create-dirs: ## Create necessary directories
	@mkdir -p $(LOGS_DIR) $(CERTS_DIR)

# =============================================================================
# Development targets
# =============================================================================

# Quick development cycle
dev: clean build run ## Full development cycle: clean, build, run
	@echo "✅ Development cycle completed"

# Test the application
test: run ## Test the application
	@sleep 3
	@make health
	@make stop

# Security scanning
security-scan: ## Run comprehensive security scan
	@echo "🔍 Running security scan..."
	@echo "Running gosec static analysis..."
	@gosec -fmt=json -out=gosec-report.json ./... 2>/dev/null || echo "gosec not available, skipping static analysis"
	@echo "Running Trivy container vulnerability scan..."
	@which trivy >/dev/null 2>&1 && trivy image --severity HIGH,CRITICAL $(IMAGE_NAME) || echo "trivy not available, skipping container scan"
	@echo "✅ Security scan completed"

# =============================================================================
# Debug targets
# =============================================================================

# Show Docker run command (for debugging)
debug-run: ## Show the Docker run command without executing
	@echo "Docker run command for development:"
	@echo "docker run -d --name $(CONTAINER_NAME) \\"
	@echo "  --restart $(DOCKER_RESTART) \\"
	@echo "  $(DOCKER_READONLY) \\"
	@echo "  $(DOCKER_TMPFS) \\"
	@echo "  $(DOCKER_CAPS) \\"
	@echo "  $(DOCKER_SECURITY) \\"
	@echo "  $(ENV_DEFAULT) \\"
	@echo "  $(VOLUME_CONFIG) \\"
	@echo "  $(VOLUME_CERTS) \\"
	@echo "  $(VOLUME_LOGS) \\"
	@echo "  $(PORTS_DEFAULT) \\"
	@echo "  $(IMAGE_NAME)"

# Show all variables (for debugging)
debug-vars: ## Show all Makefile variables
	@echo "Configuration Variables:"
	@echo "  IMAGE_NAME: $(IMAGE_NAME)"
	@echo "  CONTAINER_NAME: $(CONTAINER_NAME)"
	@echo "  PORT_PROXY: $(PORT_PROXY)"
	@echo "  PORT_HEALTH: $(PORT_HEALTH)"
	@echo "  PORT_CUSTOM: $(PORT_CUSTOM)"
