# ---------- Stage 1: 后端 ----------
# 默认走官方代理;国内构建可覆盖: docker build --build-arg GOPROXY=https://goproxy.cn,direct .
FROM golang:1.26-alpine AS backend
WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY} \
    CGO_ENABLED=0 GOOS=linux
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags="-s -w" -o /out/gateway ./cmd/gateway
RUN go build -ldflags="-s -w" -o /out/edge ./cmd/edge

# ---------- Stage 2: 用户端前端 ----------
FROM node:20-alpine AS user-web
WORKDIR /web
ARG NPM_REGISTRY=https://registry.npmjs.org
COPY web/user/package.json web/user/package-lock.json* ./
RUN if [ -n "$NPM_REGISTRY" ]; then npm config set registry "$NPM_REGISTRY"; fi && \
    npm install --no-audit --no-fund
COPY web/user/ ./
RUN npm run build

# ---------- Stage 3: 管理端前端 ----------
FROM node:20-alpine AS admin-web
WORKDIR /web
ARG NPM_REGISTRY=https://registry.npmjs.org
COPY web/admin/package.json web/admin/package-lock.json* ./
RUN if [ -n "$NPM_REGISTRY" ]; then npm config set registry "$NPM_REGISTRY"; fi && \
    npm install --no-audit --no-fund
COPY web/admin/ ./
RUN npm run build

# ---------- Stage 4: 运行 ----------
FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app
WORKDIR /app
COPY --from=backend /out/gateway /app/gateway
COPY --from=backend /out/edge /app/edge
COPY --from=user-web /web/dist /app/web/user/dist
COPY --from=admin-web /web/dist /app/web/admin/dist
COPY config.example.yaml /app/config.yaml
RUN mkdir -p /app/data/files && chown -R app:app /app
USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/gateway"]
CMD ["-config", "/app/config.yaml"]
