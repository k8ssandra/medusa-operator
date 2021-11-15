# Changelog

Changelog for medusa-operator. New PRs should update the `master / unreleased` section with entries in the order:

```markdown
* [CHANGE]
* [FEATURE]
* [ENHANCEMENT]
* [BUGFIX]
```

## Unreleased

## v0.4.0 - 2021-11-15
* [CHANGE] [#58](https://github.com/k8ssandra/medusa-operator/pull/58) Update the Medusa protobuf format to include the topology
* [FEATURE] [#61](https://github.com/k8ssandra/medusa-operator/issues/61) Allow specification of backup type (full vs differential) when creating a new backup
* [BUGFIX] [#62](https://github.com/k8ssandra/medusa-operator/issues/62) Medusa-operator deployments on k8s v1.22 fail
* [BUGFIX] [#59](https://github.com/k8ssandra/medusa-operator/pull/59) Change install directory for Fossa CLI in license-check workflow

## v0.3.3 - 2021-06-22
* [CHANGE] #46 Integrate Fossa component/license scanning
* [ENHANCEMENT] #48 Avoid extra rolling restart during restore 

## v0.3.2 - 2021-05-24
* [BUGFIX] #44 Fix JSON patch script for CRD

## v0.3.1 - 2021-05-20
* [BUGFIX] #42 Fix CRD version upgrade

## v0.3.0 - 2021-05-20
* [CHANGE] #39 Upgrade to cass-operator 1.7.0

## v0.2.1 - 2021-05-19
* [BUGFIX] #37 Avoid extra rolling restart during in-place restore

## v0.2.0 - 2021-04-07

* [CHANGE] #28 Add restore to the controller integration test
* [CHANGE] #27 Upgrade to cass-operator 1.6.0
* [BUGFIX] #32 Fix race conditions in backup controller that could result in multiple backup operations being kicked off.

