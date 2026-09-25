# Changelog

All notable changes to this project will be documented in this file. See [standard-version](https://github.com/conventional-changelog/standard-version) for commit guidelines.

## [0.11.16](https://github.com/Josh-Archer/terraform-provider-chaptarr/compare/v0.11.15...v0.11.16) (2026-09-25)


### Bug Fixes

* **provider:** escape database DSN parameters and validate ssl mode ([#86](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/86)) ([469d568](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/469d568f4687e9b38a364f3720b8cd67e85cf71c))
* **provider:** grant app role to admin user in database setup ([#84](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/84)) ([49854bd](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/49854bdcea73b9b376f84ad28442560f2bd08482))
* **provider:** handle database setup password and grant errors ([#85](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/85)) ([5554d54](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/5554d545323aba0cf86c240fbc7d12196e5109a8))


### Build System

* **deps:** bump the codeql-action group with 3 updates ([#76](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/76)) ([767231c](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/767231c8f6f6e0db4059269c88b42114f8c3a4ce))

## [0.11.15](https://github.com/Josh-Archer/terraform-provider-chaptarr/compare/v0.11.14...v0.11.15) (2026-09-17)


### Build System

* **deps:** bump google.golang.org/grpc from 1.83.1 to 1.83.2 ([#73](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/73)) ([f2a0f89](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/f2a0f8968cbb59f6efb22bf1aa1fa04ad7c5bd24))
* **deps:** bump the codeql-action group with 3 updates ([#74](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/74)) ([02a4596](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/02a4596ee3da3c0d7de71d2c6c8f51fdc42df6f0))

## [0.11.14](https://github.com/Josh-Archer/terraform-provider-chaptarr/compare/v0.11.13...v0.11.14) (2026-09-03)


### Build System

* **deps:** bump google.golang.org/grpc from 1.82.1 to 1.83.1 ([#71](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/71)) ([f33e390](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/f33e3909eb873acc81a945d01b585fc3b8f30e82))

## [0.11.13](https://github.com/Josh-Archer/terraform-provider-chaptarr/compare/v0.11.12...v0.11.13) (2026-08-31)


### Build System

* **deps:** bump the codeql-action group with 3 updates ([#69](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/69)) ([8203b8f](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/8203b8f22ecabfd0d8028e4c3ca2035ce08ef8a8))

## [0.11.12](https://github.com/Josh-Archer/terraform-provider-chaptarr/compare/v0.11.11...v0.11.12) (2026-08-27)


### Features

* **provider:** implement remaining planned read-only data sources ([#67](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/67)) ([6d9b3a6](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/6d9b3a6d70da081384fead8991d3a7f8ec6bd5b7))

## [0.11.11](https://github.com/Josh-Archer/terraform-provider-chaptarr/compare/v0.11.10...v0.11.11) (2026-08-27)


### Bug Fixes

* **ci:** configure goreleaser mode: append for existing releases ([#64](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/64)) ([295eaf6](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/295eaf658b1ec2d761daceaeb6e255f1efdde86d))
* **ci:** support unsigned asset validation and fix registry query url ([#66](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/66)) ([077b613](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/077b613a460b9fceecc70ea67e02fcd64ac71470))

## [0.11.10](https://github.com/Josh-Archer/terraform-provider-chaptarr/compare/v0.11.9...v0.11.10) (2026-08-26)


### Features

* sync OpenAPI contract to Chaptarr v0.9.929 and update CodeQL to v4.37.8 ([#61](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/61)) ([078a2a2](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/078a2a2759faa878a441ab9f1b9992d9f8656878))


### Bug Fixes

* **ci:** check release asset completeness and validate assets in release.yml ([#63](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/63)) ([ffb84ea](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/ffb84ea9b693368dcf36b77a3f4fe7288155f380))

## [0.11.9](https://github.com/Josh-Archer/terraform-provider-chaptarr/compare/v0.11.8...v0.11.9) (2026-08-24)


### Bug Fixes

* **ci:** remove environment gate from release.yml for zero-click publishing ([#55](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/55)) ([bde2a63](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/bde2a6360d4f5c8588445531e09a12454fb0df31))


### Build System

* **deps:** bump googleapis/release-please-action from 4.1.3 to 5.0.0 ([#60](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/60)) ([2c32033](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/2c3203367edb6afcc60c096c1d3f690214696601))

## [0.11.8](https://github.com/Josh-Archer/terraform-provider-chaptarr/compare/v0.11.7...v0.11.8) (2026-08-21)


### Features

* **ci:** release automation with release-please, tag-on-merge, and registry verification ([#53](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/53)) ([bde6387](https://github.com/Josh-Archer/terraform-provider-chaptarr/commit/bde638720b3fb074385ee2ff157a987133c6b761))

## [0.11.7](https://github.com/Josh-Archer/terraform-provider-chaptarr/releases/tag/v0.11.7) (2026-08-20)

### Bug Fixes
* quality-profile Update merges GET names, accepts Unknown Text, and preserves computed attributes.
