# syntax=docker/dockerfile:1.7

ARG GO_IMAGE=golang:1.25-bookworm
ARG RUNTIME_IMAGE=debian:bookworm-slim

FROM ${GO_IMAGE} AS builder

WORKDIR /src/backend

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64

# 1. 统一构建所有需要部署的后端服务二进制，避免每个服务维护一份 Dockerfile。
# 2. 输出目录固定为 /out，便于运行时镜像按命令复用同一套产物。
RUN --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    for service in userauth notification active-scheduler schedule task task-class course memory agent taskclassforum tokenstore llm api; do \
      CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o "/out/${service}" "./cmd/${service}"; \
    done

FROM ${RUNTIME_IMAGE} AS runtime

WORKDIR /app/backend

# 1. 运行时只保留证书与时区数据，保证 HTTPS 请求与国内时区配置可用。
# 2. 默认附带一份容器配置模板，实际部署仍可通过挂载文件覆盖。
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out /app/bin
COPY config.docker.yaml /app/backend/config.docker.yaml

ENV TZ=Asia/Shanghai
ENV SMARTFLOW_CONFIG_FILE=/app/backend/config.docker.yaml

CMD ["/app/bin/api"]
