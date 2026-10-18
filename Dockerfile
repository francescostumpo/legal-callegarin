FROM node:24.21.0-bookworm-slim@sha256:2fe369e969550cde8e867afc3fe370b260140cab4a23d467074295b42163d553 AS frontend

WORKDIR /app
COPY package.json package-lock.json ./
COPY web/admin/package.json ./web/admin/package.json
RUN npm ci
COPY web/admin ./web/admin
COPY scripts/verify-assets.mjs ./scripts/verify-assets.mjs
COPY internal/webassets/covers ./internal/webassets/covers
RUN npm run build

FROM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=frontend /app/internal/webassets/admin/dist ./internal/webassets/admin/dist
ARG VERSION
ARG COMMIT
RUN case "${VERSION}" in ''|*[!A-Za-z0-9._+-]*) exit 2 ;; esac; \
    case "${COMMIT}" in ''|*[!A-Za-z0-9._+-]*) exit 2 ;; esac; \
    test "${#VERSION}" -le 64; \
    test "${#COMMIT}" -le 64
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.buildVersion=${VERSION} -X main.buildCommit=${COMMIT}" -o /out/legal-callegarin ./cmd/web

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS runtime

COPY --from=builder /out/legal-callegarin /legal-callegarin
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/legal-callegarin"]
