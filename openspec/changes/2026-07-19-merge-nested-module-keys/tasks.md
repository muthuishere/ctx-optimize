# Tasks
- [x] cmdMerge dir args resolve through resolveScope (same as every verb): module dir → nested storeKey; standalone dir → name/basename as today
- [x] cmdMerge bare-name args: try SanitizeKeyPath (store-relative, slashes preserved) before the flattening SanitizeKey
- [x] hermetic test: merge two declared-module stores by dir path AND by store-relative key; single-module merge unchanged
- [x] scenario S25 flips from known-limitation to verified (proof/scenarios/run.sh)
- [x] docs: remove the limitation notes (monorepos.md, cookbook.md)
- [x] task ci + task golden green
