# The web and Go stages run on the build machine and cross-compile, so an
# arm64 image built on an amd64 runner does not emulate pnpm or the Go compiler.
FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web
WORKDIR /src/web
RUN corepack enable && corepack prepare pnpm@11.9.0 --activate
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
COPY shared/ /src/shared/
RUN pnpm run build

FROM --platform=$BUILDPLATFORM golang:1.27.1-bookworm AS go
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY config/ config/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /api ./cmd/api

FROM public.ecr.aws/awsguru/aws-lambda-adapter:0.9.1 AS adapter
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=adapter /lambda-adapter /opt/extensions/lambda-adapter
COPY --from=go /api /app/api
COPY --from=web /src/web/dist /app/web
ENV PORT=8080 WEB_DIR=/app/web DATA_DIR=/tmp/data AWS_LWA_READINESS_CHECK_PATH=/healthz
EXPOSE 8080
ENTRYPOINT ["/app/api"]
