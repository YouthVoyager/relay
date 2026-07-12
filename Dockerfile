# ---- 阶段 1:构建前端 ----
FROM node:20-alpine AS webbuild
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- 阶段 2:构建 Go ----
FROM golang:1.23-alpine AS gobuild
WORKDIR /src
# 先只拷 go.mod/go.sum 装依赖——利用层缓存:代码天天改,依赖不常变
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0:纯静态二进制,不依赖任何系统库——能跑在"空"镜像里
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /relay ./cmd/server

# ---- 阶段 3:运行镜像 ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S relay && adduser -S relay -G relay
WORKDIR /app
COPY --from=gobuild /relay ./relay
COPY --from=webbuild /src/web/dist ./web/dist
COPY migrations ./migrations
USER relay
EXPOSE 8080
ENTRYPOINT ["./relay"]