# Changelog

## [0.1.0](https://github.com/unmango/enclave/compare/v0.0.1...v0.1.0) (2026-09-25)


### Features

* add a Helm chart and publish it to ghcr.io/unmango/charts ([#17](https://github.com/unmango/enclave/issues/17)) ([400ad91](https://github.com/unmango/enclave/commit/400ad914317104570d340a57477e98766bf8bdc8))
* add a kustomize configuration pinned to the released image ([#18](https://github.com/unmango/enclave/issues/18)) ([bdf3d8b](https://github.com/unmango/enclave/commit/bdf3d8bb1e7dc036dd6c3d0bb874c1d6186e7525))
* add Kubernetes controller manager deployment and RBAC configuration ([9fe6c90](https://github.com/unmango/enclave/commit/9fe6c904a6fc8d7bf0786735a7e6c93bfeef47df))
* add Kubernetes RBAC configuration and Go module setup ([9fe6c90](https://github.com/unmango/enclave/commit/9fe6c904a6fc8d7bf0786735a7e6c93bfeef47df))
* **api:** add Enclave, EnclavePool and EnclaveClaim v1alpha1 types ([#2](https://github.com/unmango/enclave/issues/2)) ([0bc8b25](https://github.com/unmango/enclave/commit/0bc8b259fe95e18bf8dd2c558bad6b8284abb4b6))
* bind EnclaveClaims to Enclaves and project their Secrets ([#5](https://github.com/unmango/enclave/issues/5)) ([4ab0ff8](https://github.com/unmango/enclave/commit/4ab0ff8853d17e64bdb2ee4618e8cc3cdd386f07))
* initialize kubebuilder project with manager setup and metrics configuration ([9fe6c90](https://github.com/unmango/enclave/commit/9fe6c904a6fc8d7bf0786735a7e6c93bfeef47df))
* keep EnclavePools filled with warm Enclaves ([#4](https://github.com/unmango/enclave/issues/4)) ([4fe954e](https://github.com/unmango/enclave/commit/4fe954ebca601681b970cf36f33ceb632d539aca))
* reconcile Enclaves into a Pod, workspace, claim Secret and ServiceAccount ([#3](https://github.com/unmango/enclave/issues/3)) ([a038f34](https://github.com/unmango/enclave/commit/a038f34e14d36a0c2a1f329e2373e73e4004bb3b))


### Bug Fixes

* **api:** limit Enclave names to 63 characters ([#13](https://github.com/unmango/enclave/issues/13)) ([7b98a16](https://github.com/unmango/enclave/commit/7b98a16b0ccfd7d860d654505e239784e9b19d1c))
* check each roleRef in its own policy expression ([#15](https://github.com/unmango/enclave/issues/15)) ([7950d20](https://github.com/unmango/enclave/commit/7950d206970c3e372b98cfd0f2aaf4b984d63694))
* **deps:** update module github.com/onsi/gomega to v1.44.0 ([#9](https://github.com/unmango/enclave/issues/9)) ([345b01c](https://github.com/unmango/enclave/commit/345b01cc6570cefbb62b36883087095650e86438))
