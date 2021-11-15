# Medusa Operator

Medusa Operator is a Kubernetes operator that provides backup/restore capabilities for Apache Cassandra&reg; using [Medusa for Apache Cassandra&reg;](https://github.com/thelastpickle/cassandra-medusa). The operator works directly with the `CassandraDatacenter` custom resource provided by [Cass Operator](https://github.com/k8ssandra/cass-operator).

## Dependencies

For information on the packaged dependencies of Medusa Operator and their licenses, check out our [open source report](https://app.fossa.com/reports/4525e1ae-1341-411c-abf4-4eec2d36dd8e).

### Upgrading Kubernetes Dependencies

When upgrading Kubernetes Go modules, you're likely to get the following error:

```
go get: k8s.io/kubernetes@v1.21.4 requires
	k8s.io/component-helpers@v0.0.0: reading k8s.io/component-helpers/go.mod at revision v0.0.0: unknown revision v0.0.0
```

In this case, run the `scripts/download-deps.sh` script with your target Kubernetes version as argument.  
For example:  

```
./scripts/download-deps.sh v1.21.4
```

It will download the missing modules and update the `go.mod` file accordingly.
Run `go mod tidy` after that to update the `go.sum` file.