# Changelog

Changelog for medusa-operator. New PRs should update the `master / unreleased` section with entries in the order:

```markdown
* [CHANGE]
* [FEATURE]
* [ENHANCEMENT]
* [BUGFIX]
```

## master / unreleased
* [CHANGE] #35 Do not use the manager client in tests

## v0.2.0 - 2021-04-07

* [CHANGE] #28 Add restore to the controller integration test
* [CHANGE] #27 Upgrade to cass-operator 1.6.0
* [BUGFIX] #32 Fix race conditions in backup controller that could result in multiple backup operations being kicked off.

## v0.2.1 - 2021-05-19
* [BUGFIX] #37 Avoid extra rolling restart during in-place restore
