# syntax=docker/dockerfile:1.7

FROM golang:1.27.0 AS builder

WORKDIR /app

# Download module dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy only backend sources required for the build
COPY cmd ./cmd
COPY internal ./internal
COPY ent ./ent

# Generate Ent code before building
RUN go generate ./ent

# Build provenance served by GET /api/version. Passed as build args because
# .git is dockerignored; defaults match internal/buildinfo's compiled-in ones.
#   docker build --build-arg COMMIT=$(git rev-parse HEAD) ...
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown

# Build the API server binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X github.com/theimaginaryfoundation/what-iff/internal/buildinfo.Version=${VERSION} \
              -X github.com/theimaginaryfoundation/what-iff/internal/buildinfo.Commit=${COMMIT} \
              -X github.com/theimaginaryfoundation/what-iff/internal/buildinfo.BuiltAt=${BUILT_AT}" \
    -o /app/api-server ./cmd/api-server
# Prepare runtime-writable temp directory in a stage with a shell.
RUN mkdir -p /tmp/chat-app-files && chmod 775 /tmp/chat-app-files

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

COPY --from=builder --chown=nonroot:nonroot /app/api-server ./api-server
COPY --from=builder --chown=nonroot:nonroot /tmp/chat-app-files /tmp/chat-app-files

ENV SERVER_HOST=0.0.0.0 \
    SERVER_PORT=8080

EXPOSE 8080

# Note: Docker HEALTHCHECK not included (distroless has no shell/wget)
# ALB target group performs health checks via HTTP GET /api/health

ENTRYPOINT ["/app/api-server"]
