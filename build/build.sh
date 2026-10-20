#!/usr/bin/env bash
# 一键构建入口。
#
#   ./build.sh agent      交叉编译路由器控制代理 ragentd -> dist/ragentd
#   ./build.sh cloud      编译云端中继服务 -> dist/cloudsrv
#   ./build.sh firmware   编译完整 ImmortalWrt 固件（首次约 1~3 小时）
#   ./build.sh all        依次执行上述三项
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${ROOT}/dist"
mkdir -p "${DIST}"

# CI 可调：JOBS 并行度，VERBOSE 日志级别（V=s 全量 / V=sc 精简且出错即停）
JOBS="${JOBS:-$(nproc)}"
VERBOSE="${VERBOSE:-V=sc}"

OPENWRT_DIR="${ROOT}/.build/openwrt"
OPENWRT_REPO="${OPENWRT_REPO:-https://github.com/immortalwrt/immortalwrt}"
OPENWRT_BRANCH="${OPENWRT_BRANCH:-openwrt-24.10}"
TARGET="${TARGET:-mediatek/filogic}"
PROFILE="${PROFILE:-xiaomi_redmi-router-ax6000}"

build_agent() {
  echo "==> 交叉编译 ragentd (linux/arm64)"
  cd "${ROOT}/router-agent"
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w" -o "${DIST}/ragentd" ./cmd/ragentd
  ls -lh "${DIST}/ragentd"
}

build_cloud() {
  echo "==> 编译 cloudsrv (linux/amd64)"
  cd "${ROOT}/cloud"
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w" -o "${DIST}/cloudsrv" ./cmd/cloudsrv
  ls -lh "${DIST}/cloudsrv"
}

prepare_openwrt() {
  if [[ ! -d "${OPENWRT_DIR}" ]]; then
    echo "==> 拉取 ${OPENWRT_REPO} (${OPENWRT_BRANCH})"
    mkdir -p "$(dirname "${OPENWRT_DIR}")"
    git clone --depth 1 -b "${OPENWRT_BRANCH}" "${OPENWRT_REPO}" "${OPENWRT_DIR}"
  fi
  cd "${OPENWRT_DIR}"
  ./scripts/feeds update -a
  ./scripts/feeds install -a

  # 集成 OpenAppFilter（DPI 应用识别，用于 App 访问限制）
  if [[ ! -d package/OpenAppFilter ]]; then
    echo "==> 集成 OpenAppFilter"
    git clone --depth 1 https://github.com/destan19/OpenAppFilter package/OpenAppFilter
  fi

  # 集成自研控制代理
  rm -rf package/ragent
  mkdir -p package/ragent
  cp -r "${ROOT}/firmware/package/ragent/." package/ragent/
  mkdir -p package/ragent/files
  cp "${DIST}/ragentd" package/ragent/files/ragentd
  chmod +x package/ragent/files/ragentd

  # 追加配置：目标机型 + 依赖包（已追加过则跳过，避免重复 CONFIG 行）
  if ! grep -q "CONFIG_PACKAGE_ragent=y" .config 2>/dev/null; then
    cat "${ROOT}/build/config/ax6000.seed" >> .config
  fi
  make defconfig
}

build_firmware() {
  build_agent
  prepare_openwrt
  cd "${OPENWRT_DIR}"
  echo "==> 开始编译固件（JOBS=${JOBS}，VERBOSE=${VERBOSE}）"
  make -j"${JOBS}" "${VERBOSE}" 2>&1 | tee "${ROOT}/build.log"
  mkdir -p "${DIST}/firmware"
  # mediatek/filogic 的产物后缀可能是 .itb（sysupgrade）或 .bin
  find bin/targets -type f \( -name "${PROFILE}*.itb" -o -name "${PROFILE}*.bin" \) \
       -exec cp -f {} "${DIST}/firmware/" \; 2>/dev/null || true
  if [[ -z "$(ls -A "${DIST}/firmware" 2>/dev/null)" ]]; then
    echo "!! 未找到 ${PROFILE} 的产物，请检查 build.log"
    exit 1
  fi
  ls -lh "${DIST}/firmware"
}

case "${1:-all}" in
  agent)    build_agent ;;
  cloud)    build_cloud ;;
  firmware) build_firmware ;;
  all)      build_agent; build_cloud ;;
  *)        echo "用法: $0 {agent|cloud|firmware|all}"; exit 1 ;;
esac
