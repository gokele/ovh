# OVH 驾驶舱 · App(React Native / Expo)

网页控制台的随身终端:机器控制台 + 补货雷达 + 抢购队列 + 账户健康。

## 开发

```bash
npm install
npx expo start          # Expo Go 扫码预览(手机装 Expo Go)
npx expo run:ios        # 真机构建
npm run typecheck       # tsc --noEmit
```

## 配对(连接你自己的后端)

1. 电脑端打开网页控制台 → 设置 → **App 配对** → 生成配对码(2 分钟有效、一码一机)
2. 本 App 填后端地址(如 `http://192.168.1.10:19998`)+ 8 位码 → 配对
3. 令牌存手机 SecureStore(iOS Keychain);丢了手机在网页端**单独吊销**,不影响其他设备

后端与 App 不在同一网络时连不上 —— 配对地址按网页当前访问地址生成。

## 结构

```
src/
  api/       连接(SecureStore)、轮询 hooks、生物识别闸
  theme/     双主题 token(浅色默认,深色跟随系统;与 web 端两套校准值一致)
  screens/   Pairing / Machines / ServerDetail(四 Tab)/ Radar / Queue / Profile
  components/TabBar、MrtgSpark(victory-native)
../packages/core/   共享逻辑层(@core/*:可用性白名单/机房表/API 客户端/类型)
```

依赖 monorepo:`metro.config.js` 里的 `@core` 别名与 `tsconfig.json` 的 `paths`
两处必须同步改(注释已写)。

## 约定

- 白色优先,深色为可选主题(kl 红线)
- 图标 lucide-react-native,零 Emoji
- 危险动作三闸:确认对话 → Face ID(没录生物识别回退系统密码)→ 执行
- 卡片非零圆角;触达 ≥44pt;无紫蓝渐变
