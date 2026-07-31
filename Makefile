.PHONY: dev-deps backend frontend test build images manifests

# Start local PostgreSQL + MinIO
dev-deps:
	docker compose up -d

# Run the Go API against local deps (uploads staged in MinIO)
backend:
	cd backend && \
	DATABASE_URL=postgres://budgeit:budgeit@localhost:5432/budgeit \
	BUCKET_HOST=localhost BUCKET_PORT=9000 BUCKET_NAME=budgeit-uploads \
	BUCKET_USE_SSL=false \
	AWS_ACCESS_KEY_ID=budgeit AWS_SECRET_ACCESS_KEY=budgeit-secret \
	SESSION_SECRET=dev-secret-change-me \
	go run ./cmd/server

# Run the Vite dev server (proxies /api to :8080)
frontend:
	cd frontend && npm run dev

test:
	cd backend && go vet ./... && go test ./...

# Store tests that need a real PostgreSQL (first-login admin claim, cascading
# deletes). Skipped by `make test`; run `make dev-deps` first.
test-integration:
	cd backend && TEST_DATABASE_URL=postgres://budgeit:budgeit@localhost:5432/budgeit \
	go test -race ./internal/store/...

build:
	cd backend && go build ./...
	cd frontend && npm run build

images:
	podman build -t budgeit-backend:local backend
	podman build -t budgeit-frontend:local frontend

# Render the OpenShift manifests without applying them
manifests:
	kubectl kustomize deploy/overlays/prod
