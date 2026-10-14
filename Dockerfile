# 双运行模式之容器形态。最终镜像 = distroless static + 单静态二进制(前端已 go:embed)。
# 此 Dockerfile 自包含从源码构建(docker build 全链路);发版流水线用 Dockerfile.goreleaser
# 直接包装 GoReleaser 已交叉编译好的二进制(避免每架构重跑前端构建)。

# ---- 前端构建 ----
FROM node:22-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm config set registry https://registry.npmmirror.com && npm ci
COPY web/ ./
RUN npm run build

# ---- Go 静态构建 ----
FROM golang:1.26-alpine AS build
ENV CGO_ENABLED=0 \
    GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=off \
    GOTOOLCHAIN=local
WORKDIR /app
# 版本元数据由构建方传入(docker build --build-arg VERSION=...);缺省为 v1.0.0。
ARG VERSION=v1.0.0
ARG COMMIT=none
ARG DATE=unknown
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/web/dist ./web/dist
RUN go build \
    -ldflags "-s -w \
      -X github.com/atomlust/atomgo/internal/version.Version=${VERSION} \
      -X github.com/atomlust/atomgo/internal/version.Commit=${COMMIT} \
      -X github.com/atomlust/atomgo/internal/version.Date=${DATE}" \
    -o /atomgo ./cmd/atomgo
# 预建数据目录,归 nonroot(65532)所有:具名卷挂到 /data 会继承此属主,免手动 chown。
RUN mkdir -p /data

# ---- 运行镜像 ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /atomgo /atomgo
COPY --from=build --chown=65532:65532 /data /data
# 工作目录设为 /data:sqlite(atomgo.db)与相对路径产物默认落此,持久化卷一挂即生效。
WORKDIR /data
EXPOSE 7088
ENTRYPOINT ["/atomgo"]
