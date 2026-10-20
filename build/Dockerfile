# 红米 AX6000 定制固件 / 控制代理 的构建环境
#
# 用法（在任意装有 Docker 的机器上）：
#   docker build -t ax6000-builder -f build/Dockerfile .
#   docker run --rm -v $PWD:/work ax6000-builder agent      # 只交叉编译控制代理（分钟级）
#   docker run --rm -v $PWD:/work ax6000-builder firmware   # 完整固件（小时级）
#   docker run --rm -v $PWD:/work ax6000-builder cloud      # 云端服务二进制
#
# Windows 用户若未启用 WSL2/WSL1，请安装 Docker Desktop 后使用上述命令。
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# OpenWrt/ImmortalWrt 构建依赖
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential clang flex bison g++ gawk gcc-multilib g++-multilib \
        gettext git libncurses-dev libssl-dev python3 python3-distutils \
        python3-setuptools rsync swig unzip zlib1g-dev file wget curl \
        ca-certificates xz-utils zstd subversion qemu-utils ccache \
        libelf-dev ecj fastjar java-properties-common \
    && rm -rf /var/lib/apt/lists/*

# Go 1.22（交叉编译 ragentd 到 MT7986A / aarch64）
ARG GO_VERSION=1.22.5
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz -o /tmp/go.tgz \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz \
    && go version

WORKDIR /work
COPY . /work

# 交叉编译参数：MT7986A 为 ARMv8(Cortex-A53)，纯 Go 静态二进制
# 缓存放容器内目录，避免挂在 Windows NTFS 卷上导致构建变慢
ENV GOOS=linux
ENV GOARCH=arm64
ENV CGO_ENABLED=0
ENV GOPATH=/go
ENV GOCACHE=/root/.cache/go-build
ENV GOFLAGS=-trimpath

# Windows 检出时脚本可能是 CRLF 换行，统一转成 LF，否则 bash 会报 \r: command not found
RUN sed -i 's/\r$//' /work/build/build.sh \
    && chmod +x /work/build/build.sh

ENTRYPOINT ["/work/build/build.sh"]
CMD ["all"]
