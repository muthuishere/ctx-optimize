#!/usr/bin/env node
// Thin launcher for the capture companion: resolves the platform sub-package
// installed via optionalDependencies and execs its ctx-optimize-adapters
// binary (shipped beside ctx-optimize in the same bin/). No postinstall.
"use strict";
const { spawn } = require("child_process");
const path = require("path");

const platformMap = {
  "darwin-arm64": "@muthuishere/ctx-optimize-darwin-arm64",
  "darwin-x64": "@muthuishere/ctx-optimize-darwin-x64",
  "linux-arm64": "@muthuishere/ctx-optimize-linux-arm64",
  "linux-x64": "@muthuishere/ctx-optimize-linux-x64",
  "win32-x64": "@muthuishere/ctx-optimize-windows-x64",
};

const key = `${process.platform}-${process.arch}`;
const pkg = platformMap[key];
if (!pkg) {
  console.error(`ctx-optimize-adapters: unsupported platform ${key}`);
  process.exit(1);
}

const exe =
  process.platform === "win32" ? "ctx-optimize-adapters.exe" : "ctx-optimize-adapters";
let binPath;
try {
  binPath = path.join(path.dirname(require.resolve(`${pkg}/package.json`)), "bin", exe);
} catch {
  console.error(
    `ctx-optimize-adapters: platform package ${pkg} is not installed.\n` +
      `Your package manager skipped an optional dependency — reinstall with:\n` +
      `  npm install -g @muthuishere/ctx-optimize`
  );
  process.exit(1);
}

const child = spawn(binPath, process.argv.slice(2), { stdio: "inherit" });
child.on("exit", (code, signal) => {
  if (signal) process.kill(process.pid, signal);
  process.exit(code === null ? 1 : code);
});
child.on("error", (err) => {
  console.error(`ctx-optimize-adapters: failed to start binary: ${err.message}`);
  process.exit(1);
});
