#!/bin/bash
# 一键构建并安装 OVH 控制台到连接的 iPhone(绕过 Xcode 界面,产物一定是最新代码)
# 用法:手机用数据线连到 Mac → 终端执行  ./app/build-install.sh
set -e
cd "$(dirname "$0")/OvhConsole"
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer

echo "==> 构建(真机 + 自动签名)..."
xcodebuild -project OvhConsole.xcodeproj -scheme OvhConsole -configuration Debug \
  -destination 'generic/platform=iOS' \
  DEVELOPMENT_TEAM=MSCPHXMXL7 CODE_SIGN_STYLE=Automatic \
  -allowProvisioningUpdates \
  -derivedDataPath /tmp/ovh-device build 2>&1 | grep -E "error:|BUILD" || true

APP=$(find /tmp/ovh-device/Build/Products -name "OvhConsole.app" -path "*iphoneos*" | head -1)
if [ -z "$APP" ]; then echo "!! 构建失败"; exit 1; fi
echo "==> 构建产物: $APP"
echo "==> 二进制生成时间: $(stat -f '%Sm' "$APP/OvhConsole")"

echo "==> 查找已连接的 iPhone..."
UDID=$(xcrun devicectl list devices 2>/dev/null | awk '/available/ && /iPhone/ {print $4; exit}')
if [ -z "$UDID" ]; then
  echo "!! 没有找到在线的 iPhone —— 请用数据线连接手机并在手机上点\"信任\"后重试"
  exit 1
fi
echo "==> 设备: $UDID"

echo "==> 安装到手机(会覆盖旧版)..."
xcrun devicectl device install app --device "$UDID" "$APP"

echo "==> 启动 App..."
xcrun devicectl device process launch --device "$UDID" com.gokele.ovhconsole || true

echo ""
echo "✅ 完成。打开 App 的 设置 页,看\"关于 → 构建时间\"——应该就是刚才的构建时间。"
