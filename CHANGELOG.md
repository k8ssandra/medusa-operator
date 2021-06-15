# Changelog

Changelog for medusa-operator. New PRs should update the `master / unreleased` section with entries in the order:

```markdown
* [CHANGE]
* [FEATURE]
* [ENHANCEMENT]
* [BUGFIX]
```

## Unreleased

* [CHANGE] #46 Integrate Fossa component/license scanning

## v0.3.2
* [BUGFIX] #44 Fix JSON patch script for CRD

## v0.3.1
* [BUGFIX] #42 Fix CRD version upgrade

## v0.3.0
* [CHANGE] #39 Upgrade to cass-operator 1.7.0

## v0.2.1 - 2021-05-19
* [BUGFIX] #37 Avoid extra rolling restart during in-place restore

## v0.2.0 - 2021-04-07

* [CHANGE] #28 Add restore to the controller integration test
* [CHANGE] #27 Upgrade to cass-operator 1.6.0
* [BUGFIX] #32 Fix race conditions in backup controller that could result in multiple backup operations being kicked off.

