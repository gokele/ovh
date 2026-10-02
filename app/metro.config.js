/**
 * Metro 配置:让 @core/* 指向 packages/core/src(共享逻辑层)。
 * tsconfig paths 只对 tsc 生效,Metro 打包要在这里再声明一次 —— 两处必须同步改。
 */
const { getDefaultConfig } = require("expo/metro-config");
const path = require("path");

const projectRoot = __dirname;
const monorepoRoot = path.resolve(projectRoot, "..");

const config = getDefaultConfig(projectRoot);
// node_modules 查找范围扩到 monorepo 根(packages/core 的依赖要能被解析)
config.watchFolders = [monorepoRoot];
config.resolver.nodeModulesPaths = [
  path.resolve(projectRoot, "node_modules"),
  path.resolve(monorepoRoot, "node_modules"),
];
// @core/* → ../packages/core/src/*
config.resolver.extraNodeModules = {
  "@core": path.resolve(monorepoRoot, "packages/core/src"),
};

module.exports = config;
