# Tasks — manifest lane (blocked on W4 merge; then W5 agent)

- [ ] internal/extract/manifests producer: package.json, pom.xml, csproj/sln (+ add .csproj to walk), go.mod, gradle line-shapes
- [ ] k8s resource nodes + topology edges (shared yaml indent walker with W4's lane C); Helm-template skip
- [ ] dep:/task:/k8s:// id scheme + declares/depends_on/selects/routes_to/mounts/uses_image edges with provenance discipline
- [ ] manifest packs (.ctxoptimize/manifests/ + store-root manifests/; tiny json/xml/yaml path selectors; loud validation)
- [ ] manifests list/add/remove verbs (+ github-url install, reuse routes/grammar fetch)
- [ ] fixture monorepo tests + false-positive guards (Taskfile/.goreleaser gain nothing) + idempotent re-add + prune proof
