# VPN Xray Node Makefile
# Usage: make run, make stop, make logs, make clean

# Configuration
IMAGE_NAME = xray-node:latest
CONTAINER_NAME = xray-vpn
CONFIG_FILE = ./config.json
LOGS_DIR = ./logs
CERTS_DIR = ./certs
PORT_PROXY = 8443
PORT_HEALTH = 8080

# Build the Docker image
build:
	docker build -t $(IMAGE_NAME) .

# Run the container
run: build
	@echo "Starting VPN Xray Node..."
	@mkdir -p $(LOGS_DIR) $(CERTS_DIR)
	docker run -d --name $(CONTAINER_NAME) \
	--restart unless-stopped \
	--read-only \
	--tmpfs /tmp:rw,noexec,nosuid,size=64m \
	--tmpfs /run:rw,noexec,nosuid,size=32m \
	--cap-drop ALL \
	--cap-add NET_BIND_SERVICE \
	--security-opt "no-new-privileges:true" \
	-v $(PWD)/$(CONFIG_FILE):/etc/xray/config.json:ro \
	-v $(PWD)/$(CERTS_DIR):/etc/xray/certs:ro \
	-v $(PWD)/$(LOGS_DIR):/var/log/xray:rw \
	-p $(PORT_PROXY):443 \
	-p $(PORT_HEALTH):8080 \
	$(IMAGE_NAME)
	@echo "VPN Xray Node started on ports $(PORT_PROXY) (proxy) and $(PORT_HEALTH) (health)"

# Run with production security profiles (requires root)
run-prod: build
	@echo "Starting VPN Xray Node with production security..."
	@mkdir -p $(LOGS_DIR) $(CERTS_DIR)
	sudo docker run -d --name $(CONTAINER_NAME) \
	--restart unless-stopped \
	--read-only \
	--tmpfs /tmp:rw,noexec,nosuid,size=64m \
	--tmpfs /run:rw,noexec,nosuid,size=32m \
	--cap-drop ALL \
	--cap-add NET_BIND_SERVICE \
	--security-opt "no-new-privileges:true" \
	--security-opt seccomp=$(PWD)/seccomp.json \
	--security-opt apparmor=$(PWD)/apparmor-xray \
	-v $(PWD)/$(CONFIG_FILE):/etc/xray/config.json:ro \
	-v $(PWD)/$(CERTS_DIR):/etc/xray/certs:ro \
	-v $(PWD)/$(LOGS_DIR):/var/log/xray:rw \
	-p $(PORT_PROXY):443 \
	-p $(PORT_HEALTH):8080 \
	$(IMAGE_NAME)
	@echo "VPN Xray Node started with production security"

# Stop the container
stop:
	@echo "Stopping VPN Xray Node..."
	docker stop $(CONTAINER_NAME) || true
	docker rm $(CONTAINER_NAME) || true
	@echo "VPN Xray Node stopped"

# Show container logs
logs:
	docker logs -f $(CONTAINER_NAME)

# Show container status
status:
	docker ps -a --filter name=$(CONTAINER_NAME)

# Health check
health:
	@echo "Checking health endpoint..."
	@curl -s http://localhost:$(PORT_HEALTH)/ || echo "Health check failed"

# Clean up everything
clean: stop
	@echo "Cleaning up..."
	docker rmi $(IMAGE_NAME) || true
	@echo "Cleanup complete"

# Generate self-signed certificates
certs:
	@echo "Generating self-signed certificates..."
	@mkdir -p $(CERTS_DIR)
	openssl req -x509 -newkey rsa:4096 -keyout $(CERTS_DIR)/private.key -out $(CERTS_DIR)/certificate.crt -days 365 -nodes -subj "/C=US/ST=State/L=City/O=Organization/CN=localhost"
	@echo "Certificates generated in $(CERTS_DIR)/"

# Show certificate info
certs-info:
	@echo "Certificate Information:"
	@openssl x509 -in $(CERTS_DIR)/certificate.crt -text -noout | head -20

# API management commands
api-users:
	@echo "Listing users..."
	@curl -s http://localhost:$(PORT_HEALTH)/api/users

api-stats:
	@echo "Getting traffic statistics..."
	@curl -s http://localhost:$(PORT_HEALTH)/api/stats

api-system:
	@echo "Getting system information..."
	@curl -s http://localhost:$(PORT_HEALTH)/api/system

api-quotas:
	@echo "Getting quota information..."
	@curl -s http://localhost:$(PORT_HEALTH)/api/quotas

# Show help
help:
	@echo "VPN Xray Node Management Commands:"
	@echo "  make build     - Build the Docker image"
	@echo "  make run       - Start the container (development)"
	@echo "  make run-prod  - Start with production security (requires root)"
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
	@echo "API Management:"
	@echo "  make api-users - List all users"
	@echo "  make api-stats - Get traffic statistics"
	@echo "  make api-system - Get system information"
	@echo "  make api-quotas - Get quota information"
	@echo ""
	@echo "  make help      - Show this help"
