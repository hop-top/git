# Changelog

## [0.1.0-alpha.2](https://github.com/hop-top/git/compare/v0.1.0-alpha.1...v0.1.0-alpha.2) (2026-09-04)


### ⚠ BREAKING CHANGES

* **deps:** --verbose semantics changed.

### chore

* **deps:** migrate to kit v0.4.0-alpha.3 ([adefd83](https://github.com/hop-top/git/commit/adefd83b8fc9b58c902c7bd71ae2cc9eedc79cb7))


### Features

* **add,list:** record and display per-branch comparison base ([8b0da80](https://github.com/hop-top/git/commit/8b0da80dd39057cec3e2768a76ec240b258e64df))
* **add:** branch new worktrees from default branch by default ([16b6e08](https://github.com/hop-top/git/commit/16b6e08703de024b6154681d2f1d73c4e669c751))
* **add:** copy ignored local state into new worktrees ([4de60a5](https://github.com/hop-top/git/commit/4de60a5798e4bd0b4a49c08c18f288b94406d9f9))
* **add:** honour #-hop-# marker to exclude ignore rules from copy-ignored ([5562462](https://github.com/hop-top/git/commit/5562462127f736cd2125bedb5bd91f283e21078d))
* **add:** honour #-hop-# marker to exclude ignore rules from copy-ignored ([045afa8](https://github.com/hop-top/git/commit/045afa8d6fb37415e3e79fd01af9a290ca2b6bd3))
* **clone:** dispatch pre-clone, post-worktree-add, post-clone hooks ([225ee3a](https://github.com/hop-top/git/commit/225ee3a0d59fc1844f01ae26e92f53da2e722369))
* **git:** add WorktreeRepair, WorktreeListPorcelain, WorktreeAdd ([9b90648](https://github.com/hop-top/git/commit/9b9064834906853cb404c25c6d2008abd02927a3))
* **hooks:** add clone and switch hook names to the registry ([2bf01a6](https://github.com/hop-top/git/commit/2bf01a63e2e0a3d0016acac83c1a5d413ac62514))
* **hooks:** complete the lifecycle hook surface ([17c701b](https://github.com/hop-top/git/commit/17c701bb54121792cd8dbf99c8d11a72c5ce832f))
* **hooks:** dispatch worktree-switch hooks on branch hop ([ed51a3a](https://github.com/hop-top/git/commit/ed51a3ad6206cb723972a027302b1c783e3f43f8))
* **hooks:** extend hook env with from-state and trigger metadata ([e8c6136](https://github.com/hop-top/git/commit/e8c61366685f221478ba1ad955eaafe126792c0b))
* **hooks:** mirror committed .git-hop/hooks/ on clone and init ([02b6262](https://github.com/hop-top/git/commit/02b62622d92a9d954dc8707b38a0a7619de7f088))
* **hooks:** reserved exit code for hook-handled navigation ([e88547e](https://github.com/hop-top/git/commit/e88547ed3c10fd1d53285019c9df32d29ce322f1))
* **hop:** add advisory FileLock helper (T-0209) ([5b5de7d](https://github.com/hop-top/git/commit/5b5de7d28f20298b68b504aeb10621c240169a08))
* **list:** show branch sync status column ([#23](https://github.com/hop-top/git/issues/23)) ([3183443](https://github.com/hop-top/git/commit/31834439e554a66e9cd72865434772cb31d5b347))
* **merge:** gate source-branch remote deletion behind --delete-remote ([951e4ef](https://github.com/hop-top/git/commit/951e4ef1d93074cb65bfaf39ff5647efe3aeccb8))
* **prune:** GC stale repair backups (T-0209) ([8b17d1c](https://github.com/hop-top/git/commit/8b17d1cee2c57953ad1479b2a20197782db8fdb2))
* **release:** adopt release-please via hop-top/.github workflows ([#31](https://github.com/hop-top/git/issues/31)) ([f44bd54](https://github.com/hop-top/git/commit/f44bd54590de98e174facac0bca2b0a4700a9514))
* **remove:** add --merged flag for bulk-removing merged worktrees ([0305b20](https://github.com/hop-top/git/commit/0305b20c1f2aabcd11bff71ee1f241d8bf5a070c))
* **repair:** --base infers and records legacy HubBranch.Base ([33c8277](https://github.com/hop-top/git/commit/33c82775d043a5488fbbc934f5f828ba3db90d43))
* **repair:** add Applier with per-Action mutation handlers (T-0209) ([f2c1b4b](https://github.com/hop-top/git/commit/f2c1b4bea0eb542f60fd4d604de2f96814e45f89))
* **repair:** add metadata-only backup snapshot + manifest (T-0209) ([65777c4](https://github.com/hop-top/git/commit/65777c4f1214b2131dc9f96a759cba8ee05cd223))
* **repair:** add Plan/Action/ActionKind types (T-0209) ([ca9209b](https://github.com/hop-top/git/commit/ca9209bf374798b4c2cd2c9e9140f2aed6656cfe))
* **repair:** add Planner with read-only worktree classification (T-0209) ([b8a67f9](https://github.com/hop-top/git/commit/b8a67f978073fd152766cbe1725bfea7026ea47b))
* **repair:** add RepairCmd with full pipeline (T-0209) ([36eef3f](https://github.com/hop-top/git/commit/36eef3fc36cc3a494386d9b52d6072451824d0cd))
* **repair:** add Restore for backup undo with sha256 verification (T-0209) ([eeda02d](https://github.com/hop-top/git/commit/eeda02d799028f2a83dd0fc78f667b65fb5ef766))
* **shell:** dispatch switch hook on chdir into worktree ([04d626a](https://github.com/hop-top/git/commit/04d626a92cdaaaffef298e9ab55eea57a12633b1))
* **shell:** honor hook-handled navigation directive in wrappers ([dcc5665](https://github.com/hop-top/git/commit/dcc5665488719194e0d5367baf9000a513a627dd))


### Bug Fixes

* **cli:** route bare arg by hub lookup, not prefix allowlist ([7f42f6d](https://github.com/hop-top/git/commit/7f42f6dfbe9dc42446037cbf847851c78ef86d3d))
* **clone:** hopspace must always be bare ([cc5def4](https://github.com/hop-top/git/commit/cc5def487afd643fb8d38a972af00bb8adec10a0))
* **doctor:** drop current hub's orphan hop.json rows on --fix ([b777e59](https://github.com/hop-top/git/commit/b777e5907ad5d102f1dd00cc5d5fcf83ecdbc625))
* **doctor:** honor --dry-run under --fix ([badd4a6](https://github.com/hop-top/git/commit/badd4a62bd640cf8db7a41f0e20c01430c114ce6))
* **doctor:** resolve absolute branch paths without doubling ([e880da8](https://github.com/hop-top/git/commit/e880da88407da26834fa9cf5f386aad105621af0))
* **doctor:** respect gitignored vendor, downgrade stale symlink severity ([9f80ad2](https://github.com/hop-top/git/commit/9f80ad26216de8dac15cc4fe513b27209f018367))
* **env:** fail loudly on unanswerable gc confirmation prompt ([e341836](https://github.com/hop-top/git/commit/e341836b8aa98583fab05d1933916b242ec88319))
* **git-hop:** set upstream tracking when creating new worktree branches ([7a17a7e](https://github.com/hop-top/git/commit/7a17a7e70c5ccef81d877d13f6c518e0ce8f074c))
* **git:** correct argv shape for worktree add --track ([3592044](https://github.com/hop-top/git/commit/35920440f575f1577e114056cd9d81d1d75e73c9))
* **git:** unbreak windows build of bounded network runner ([d99c24e](https://github.com/hop-top/git/commit/d99c24eb90ee63e3e25fd3c866cbc2d966a3ce08))
* **go:** skip go mod vendor when vendor/ is gitignored ([8b776b2](https://github.com/hop-top/git/commit/8b776b2cc4ccde32074251dbb4ba7a26a8d85d0c))
* **init:** back-fill missing hop.json on idempotent re-init ([#27](https://github.com/hop-top/git/issues/27)) ([ee260ba](https://github.com/hop-top/git/commit/ee260ba5689b4aeb397c21c7ba7355e92539a7af))
* **init:** fail loudly instead of hanging on unanswerable prompt ([2786ccf](https://github.com/hop-top/git/commit/2786ccfc29b56c0a73d5a5ab107d336e155a45f0))
* **init:** handle existing .github/ in source repo during conversion (T-0166) ([a0156ab](https://github.com/hop-top/git/commit/a0156ab7d810f71ad2d9a998c12c68e1b7a2f567))
* **init:** name converted worktree after branch, not commit SHA ([e201709](https://github.com/hop-top/git/commit/e201709a5502c407a8d3f9830c83b3966b2d3eb5))
* **init:** preserve mode bits in restore-on-failure path (T-0166) ([a020c2f](https://github.com/hop-top/git/commit/a020c2f3a962a5c6939fe740b2a8215dc55ff0fe))
* **prune:** drop orphan hop.json rows, not just state.json ([ef299ad](https://github.com/hop-top/git/commit/ef299ad4dd14154880223dda95573a9afb3fc709))
* **prune:** honor --dry-run flag (T-0175) ([#24](https://github.com/hop-top/git/issues/24)) ([ee98684](https://github.com/hop-top/git/commit/ee986841b41c8d9f9d5fe85f6a9ac774b55590e5))
* **prune:** scope to current repository by default ([8d3b7f9](https://github.com/hop-top/git/commit/8d3b7f916b16703de01442e729df9814253327b0))
* **remove:** fail loudly on unanswerable confirmation prompt ([84a0dbc](https://github.com/hop-top/git/commit/84a0dbc93ab7b41d70cd9d8834986792f32057d2))
* **remove:** stop unbounded origin probe hanging removal ([6405edc](https://github.com/hop-top/git/commit/6405edca7112c69391781ba976856d4a05e9c0a7))
* **remove:** stop warning on already-absent worktree and branch ([4a0f44f](https://github.com/hop-top/git/commit/4a0f44f91147bd063ed36f46b194e0d7a7221ea4))
* **repair:** restore missing remote.origin.fetch refspec on legacy hubs ([896e960](https://github.com/hop-top/git/commit/896e960b057edcc119ff1122ec60eb8bb5f2ed9c))
* **repair:** route repair hooks through shared hook runner ([85d6eac](https://github.com/hop-top/git/commit/85d6eac283199b724c667eebd89827e28df49920))
* set upstream tracking on default branch worktree after clone ([8dd6b0e](https://github.com/hop-top/git/commit/8dd6b0e31e53f2b1ddb0a6a8f6c18c14b2bb4a44))
* **status:** accurate sync labels with dirty detail ([4db2dd9](https://github.com/hop-top/git/commit/4db2dd9dc3f305d763f8436c0af73a361edae38e))
* **status:** diagnose bare-worktree repo missing hop.json ([#26](https://github.com/hop-top/git/issues/26)) ([284ca58](https://github.com/hop-top/git/commit/284ca58b91cd4afc160472067c5c8de24a9ab78f))
* **switch:** anchor worktree path on hub, not process cwd ([8cb521a](https://github.com/hop-top/git/commit/8cb521a690f4af9a0b114bc0c0496bfea89063c4))


### Code Refactoring

* adopt kit framework ([#19](https://github.com/hop-top/git/issues/19)) ([16a9e2d](https://github.com/hop-top/git/commit/16a9e2d5d56a612a94a6d06308c269cb9e9ac604))


### Documentation

* add git hop repair to core commands table ([516f65d](https://github.com/hop-top/git/commit/516f65d47e5142d4a728d84b99963b4d9c677072))
* **add:** document copy-ignored step and #-hop-# marker across user docs ([6a96f2f](https://github.com/hop-top/git/commit/6a96f2f7df7f696a727deded969c2dd7fdc58fd3))
* apply intent-driven progressive disclosure principles ([84d1b09](https://github.com/hop-top/git/commit/84d1b091e69e26415556d0cb1079a7a6859d5189))
* **claude:** rewrite CLAUDE.md with build/test, layout, architecture, porcelain conventions ([71ba6ed](https://github.com/hop-top/git/commit/71ba6ed4fb1473d07bf05d3096d7653e63aa798e))
* **concepts:** drop private-toolchain specifics from hub-root state ([4701877](https://github.com/hop-top/git/commit/47018773cd45fa7cf13bc26c23f0dbcd453e188c))
* **concepts:** stop naming third-party tool dirs in hub layout ([c98035f](https://github.com/hop-top/git/commit/c98035f17c6b569425ba0616613c1ecb5caec20f))
* configuration.md key reference, both cheatsheets. ([951e4ef](https://github.com/hop-top/git/commit/951e4ef1d93074cb65bfaf39ff5647efe3aeccb8))
* **core-concepts:** document per-repo state dirs at hub root ([#22](https://github.com/hop-top/git/issues/22)) ([ffc2968](https://github.com/hop-top/git/commit/ffc29686336d265423b4a56c42196e9741e91c43))
* doctor --help, error-recovery, cheatsheets. Golden regenerated. ([b777e59](https://github.com/hop-top/git/commit/b777e5907ad5d102f1dd00cc5d5fcf83ecdbc625))
* **examples:** add tmux window-per-worktree hook example ([bdf526e](https://github.com/hop-top/git/commit/bdf526e6cafa16a1341dcba662ce288770f412a2))
* fill gaps around prune, remove, doctor, and env gc cleanup ([fb29bfb](https://github.com/hop-top/git/commit/fb29bfb7f54313408b90d2007baa4354f8f8dd3f))
* **hooks:** document full lifecycle surface, fix drift ([650b9e4](https://github.com/hop-top/git/commit/650b9e49b6f26a76287a3cefe4e289b04df06e17))
* **hooks:** reconcile macOS paths, repoID shape, hook-level tradeoffs ([2657820](https://github.com/hop-top/git/commit/2657820f49531a029d511fe9ab40a2f13224ff33))
* non-interactive init in agent + human cheatsheets. ([2786ccf](https://github.com/hop-top/git/commit/2786ccfc29b56c0a73d5a5ab107d336e155a45f0))
* prune scope + flags, --dry-run in flag list, stale --force removed. ([8d3b7f9](https://github.com/hop-top/git/commit/8d3b7f916b16703de01442e729df9814253327b0))
* **repair:** correct hook resolution and env for repair hooks ([3474e4d](https://github.com/hop-top/git/commit/3474e4d397b149805a5cfc1e9280022e165e49d2))
* **repair:** document git hop repair command (T-0209) ([6b3e331](https://github.com/hop-top/git/commit/6b3e331116bf1f24ac3cdaa43628db84de29ae97))

## Changelog

<!-- This file is maintained by release-please.
     Do NOT edit by hand — changes will be overwritten.
     See docs/release.md for the release workflow. -->
