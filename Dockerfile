# syntax=docker/dockerfile:1

# Image chạy server yuumi (issue #49). Runtime cần đủ bộ công cụ mà 1 review
# job dùng: git (clone PR), Go toolchain (gofmt/go vet trên repo Go) và
# Claude CLI — để sau này chính image này chạy được sandbox riêng cho mỗi
# job (issue #78 giai đoạn 2).

ARG GO_VERSION=1.26.5

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/yuumi-server ./cmd/server

# Runtime dùng luôn image golang (đã có go + git + curl): go vet cần toolchain
# thật, tự ráp Go vào image slim không tiết kiệm được bao nhiêu.
FROM golang:${GO_VERSION}-bookworm

# Ghim đúng bản đã kiểm chứng các flag bảo mật (--setting-sources,
# --strict-mcp-config, chặn đọc ngoài thư mục review — xem PR #85). Nâng
# phiên bản thì kiểm chứng lại các flag đó trước.
ARG CLAUDE_VERSION=2.1.281

# Không chạy bằng root: code PR không tin cậy được clone và chạy go vet
# trong container này.
RUN useradd --create-home --uid 10001 yuumi \
    && mkdir -p /app/logs \
    && chown -R yuumi:yuumi /app

USER yuumi
WORKDIR /app

# DISABLE_AUTOUPDATER: Claude CLI native tự cập nhật, trong container phải
# giữ đúng bản đã ghim.
ENV PATH=/home/yuumi/.local/bin:$PATH \
    DISABLE_AUTOUPDATER=1
RUN curl -fsSL https://claude.ai/install.sh | bash -s "${CLAUDE_VERSION}" \
    && claude --version

COPY --from=build /out/yuumi-server /usr/local/bin/yuumi-server

# Review log, review state, bundle cache đều ghi dưới /app/logs (đường dẫn
# mặc định tương đối "logs/..."): mount volume để không mất khi container
# bị tạo lại.
VOLUME /app/logs

EXPOSE 8080
# /health trả 503 khi Claude CLI hoặc GitHub App auth không dùng được.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s \
    CMD curl -fsS http://localhost:8080/health || exit 1

ENTRYPOINT ["yuumi-server"]
