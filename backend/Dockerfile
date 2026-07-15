# Build stage: Red Hat UBI Go toolset
FROM registry.access.redhat.com/ubi9/go-toolset:latest AS build
WORKDIR /opt/app-root/src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Static binary so the runtime image needs no libc
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /tmp/budgeit-server ./cmd/server

# Runtime stage: UBI minimal, non-root (restricted SCC compatible)
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
COPY --from=build /tmp/budgeit-server /usr/local/bin/budgeit-server
USER 1001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/budgeit-server"]
