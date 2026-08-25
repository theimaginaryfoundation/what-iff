# syntax=docker/dockerfile:1.7

FROM node:22 AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . ./

ARG API_URL=http://localhost:8080/api
ARG AUTH_MODE=local
ARG ENABLE_GALLERY_EXPRESSION_ASSIGNMENT=false
ARG DOCS_URL=https://whatiff.chat/docs.html
# Build provenance written to /version.json (scripts/write-version.mjs).
# .git is dockerignored, so COMMIT must come from a build arg here — pass it
# as e.g. --build-arg COMMIT=$(git rev-parse HEAD). BUILT_AT must be RFC
# 3339 (e.g. 2026-08-21T00:00:00Z) if supplied. Its default is intentionally
# an empty string, not a placeholder like "unknown": write-version.mjs treats
# an empty/unset BUILT_AT as "generate a real timestamp", so an empty
# default here is what lets that fallback run when a pipeline doesn't supply
# one — don't change it to a non-empty placeholder, that would suppress the
# fallback the same way COMMIT's non-empty default suppresses gitHead().
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=
ARG OVERLAY_COMMIT=

RUN sed -i "s|__API_URL__|${API_URL}|g" src/environments/environment.docker.ts && \
    sed -i "s|__AUTH_MODE__|${AUTH_MODE}|g" src/environments/environment.docker.ts && \
    sed -i "s|__ENABLE_GALLERY_EXPRESSION_ASSIGNMENT__|${ENABLE_GALLERY_EXPRESSION_ASSIGNMENT}|g" src/environments/environment.docker.ts && \
    sed -i "s|__DOCS_URL__|${DOCS_URL}|g" src/environments/environment.docker.ts

RUN env VERSION="${VERSION}" COMMIT="${COMMIT}" BUILT_AT="${BUILT_AT}" OVERLAY_COMMIT="${OVERLAY_COMMIT}" npm run build:docker

FROM nginx:1.27-alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist/what-iff/browser /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
