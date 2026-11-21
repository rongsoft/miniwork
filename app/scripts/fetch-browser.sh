#!/bin/bash
# 把一份 Chromium 下到 browser/<平台>/ 里，随应用一起分发。
#
# 为什么要内置：客户机器上装没装 Chrome 完全不可控，而「扫码登录」这件事
# 没浏览器就是死路。用户不该为了发一条笔记去装 Chrome。
#
# 用 Chrome for Testing 而不是 Chromium snapshot：
#   1. 版本固定，接口行为可预期（snapshot 每周换一次，选择器会莫名失效）；
#   2. 官方提供免安装压缩包，mac/win/linux 三平台同构，不用自己编译；
#   3. 它就是 Chrome，UA 和页面行为跟用户装的 Chrome 一致。
#
# 用法：
#   scripts/fetch-browser.sh                # 本机平台 + 最新稳定版
#   scripts/fetch-browser.sh win64          # 指定平台
#   scripts/fetch-browser.sh all            # mac 与 win64 各备一份（打包用）
#   scripts/fetch-browser.sh mac-arm64 157.0.8103.78
#
# 下载缓存放在 .cache/browser/（可随时整个删掉，下次会重下）。
#
# 只依赖 curl 和解压工具，不用 python3：Windows 上打安装包时是在那台机器上跑，
# 而 Git Bash 一般没有 python。
set -euo pipefail

cd "$(dirname "$0")/.."

PLAT="${1:-}"
WANT="${2:-}"
CACHE=".cache/browser"
mkdir -p "$CACHE" browser

# 本机平台 → Chrome for Testing 的平台名
detect_plat() {
  case "$(uname -s)" in
    Darwin)
      if [ "$(uname -m)" = "arm64" ]; then echo "mac-arm64"; else echo "mac-x64"; fi
      ;;
    MINGW*|MSYS*|CYGWIN*) echo "win64" ;;
    Linux) echo "linux64" ;;
    *) echo "未知平台：$(uname -s) $(uname -m)" >&2; exit 1 ;;
  esac
}

# 解压 zip。Windows 的 Git Bash 常常没有 unzip，但一定有 tar（libarchive，认 zip）。
extract_zip() {
  local zip="$1" dst="$2"
  if command -v unzip >/dev/null 2>&1; then
    unzip -q "${zip}" -d "${dst}"
  elif tar -xf "${zip}" -C "${dst}" 2>/dev/null; then
    :
  else
    echo "既没有 unzip 也解不开 zip，先装一个" >&2
    exit 1
  fi
}

# 版本号单独一个纯文本端点，不用解析 JSON——省掉 python 依赖。
latest_version() {
  curl -fsSL https://googlechromelabs.github.io/chrome-for-testing/LATEST_RELEASE_STABLE | tr -d ' \r\n'
}

fetch_one() {
  local plat="$1" ver="$2"
  local dest="browser/${plat}"

  if [ -f "${dest}/VERSION" ] && [ "$(cat "${dest}/VERSION")" = "${ver}" ] && [ "${FORCE:-0}" != "1" ]; then
    echo "==> ${plat} 已经是 ${ver}，跳过（要重装加 FORCE=1）"
    return 0
  fi

  local url="https://storage.googleapis.com/chrome-for-testing-public/${ver}/${plat}/chrome-${plat}.zip"
  echo "==> ${plat}  Chromium ${ver}"
  echo "    ${url}"

  local zip="${CACHE}/chrome-${plat}-${ver}.zip"
  if [ ! -s "${zip}" ]; then
    curl -fL --progress-bar -o "${zip}.part" "${url}"
    mv "${zip}.part" "${zip}"
  else
    echo "    压缩包已有缓存，跳过下载"
  fi

  # 解压到干净的 staging 目录，再整体换进去。
  # 不在这里删旧目录：目录里有几百个文件，脚本自己删风险太大，
  # 真出问题用户连回溯的机会都没有。旧的移到 .cache 里留着。
  local stage="browser/.stage-${plat}"
  rm -rf "${stage}"
  mkdir -p "${stage}"
  extract_zip "${zip}" "${stage}"

  local src="${stage}/chrome-${plat}"
  [ -d "${src}" ] || { echo "压缩包结构不对，没找到 chrome-${plat}" >&2; exit 1; }
  echo "${ver}" > "${src}/VERSION"

  if [ -e "${dest}" ]; then
    local old="${CACHE}/old-${plat}-$(date +%Y%m%d%H%M%S)"
    mv "${dest}" "${old}"
    echo "    旧版本挪到了 ${old}（要空间就直接删 .cache）"
  fi
  mv "${src}" "${dest}"
  rmdir "${stage}"

  # 可执行文件在哪：先按平台猜，猜不到就自己找。
  local exe
  case "${plat}" in
    mac-*) exe="${dest}/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing" ;;
    win64) exe="${dest}/chrome.exe" ;;
    *)     exe="${dest}/chrome" ;;
  esac
  if [ ! -f "${exe}" ]; then
    exe="$(find "${dest}" -maxdepth 4 -type f \( -name "chrome" -o -name "chrome.exe" -o -name "Google Chrome for Testing" \) | head -1)"
  fi
  [ -n "${exe}" ] && [ -f "${exe}" ] || { echo "下完了但找不到可执行文件" >&2; exit 1; }

  # 只去掉隔离属性，不重新签名。
  #
  # 这条是踩出来的：一开始在这里跑 codesign --force --deep --sign -，
  # 结果把 Google 自己签的那份（linker-signed adhoc）覆盖成纯 adhoc，
  # 嵌套的 helper 丢了 entitlements，浏览器起得来、页面却一渲染就崩。
  # Google 发的这份本身就能用，别动它。
  xattr -cr "${dest}" 2>/dev/null || true

  echo "==> 好了：${exe}"
  du -sh "${dest}"
}

[ -n "${PLAT}" ] || PLAT="$(detect_plat)"

if [ "${PLAT}" = "all" ]; then
  # 打包要用的就这两个：mac 出 .app，win64 出安装包。
  # 一次备好，省得打 Windows 包时才发现缺浏览器（Taskfile 会硬失败）。
  VER="${WANT:-latest}"
  [ "${VER}" = "latest" ] && VER="$(latest_version)"
  echo "==> 本次版本 ${VER}"
  fetch_one mac-arm64 "${VER}"
  fetch_one win64 "${VER}"
  echo "==> 两个平台都备好了"
  du -sh browser/*
  exit 0
fi

VER="${WANT:-latest}"
[ "${VER}" = "latest" ] && VER="$(latest_version)"
fetch_one "${PLAT}" "${VER}"
