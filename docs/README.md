# Create storage bucket
* GCS - https://github.com/thelastpickle/cassandra-medusa/blob/master/docs/gcs_setup.md
* S3 - https://github.com/thelastpickle/cassandra-medusa/blob/master/docs/aws_s3_setup.md

# Create bucket secret
We need a secret with credentials to making API requests to access the bucket.

GCS setup - https://github.com/thelastpickle/cassandra-medusa/blob/master/docs/gcs_setup.md

**GCS:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: medusa-gcp-key
type: Opaque
stringData:
  medusa_gcp_key.json: |-
    {
      "type": "service_account",
      "project_id": "gcp-dmc",
      "private_key_id": "7d3261b6a68300f6e3e0a25f35d3e8104e59bee3",
      "private_key": "-----BEGIN PRIVATE KEY-----\nXXXXXXXXXXXXXXXXXXXXXXXXXXXXxXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX\n-----END PRIVATE KEY-----\n",
     "client_email": "k8ssandra-medusa@gcp-example.iam.gserviceaccount.com",
     "client_id": "123456789",
     "auth_uri": "https://accounts.google.com/o/oauth2/auth",
     "token_uri": "https://oauth2.googleapis.com/token",
     "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
     "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/k8ssandra-medusa%40gcp-example.iam.gserviceaccount.com"
    }
```

**S3:**

```yaml
apiVersion: v1
kind: Secret
metadata:
 name: medusa-bucket-key
type: Opaque
stringData:
 # Note that this currently has to be set to medusa_s3_credentials!
 medusa_s3_credentials: |-
   [default]
   aws_access_key_id = my_access_key
   aws_secret_access_key = my_secret_key
```

# Medusa configuration
```
# medusa.ini

[cassandra]
# The start and stop commands are not applicable in k8s.
stop_cmd = /etc/init.d/cassandra stop
start_cmd = /etc/init.d/cassandra start
cql_username = cassandra
cql_password = cassandra
check_running = nodetool version

[storage]
# Update this based on the region you are using. The values are
# provided by libcloud. See https://github.com/thelastpickle/cassandra-medusa/blob/master/medusa/storage/s3_storage.py.
storage_provider = s3

# Set this to the name of your S3/GCS bucket
bucket_name = k8ssandra-medusa-dev

# The file name at the end of this path needs to match the property name 
# in the bucket secet. In the S3 secret above, the property name is
# medusa_s3_credentials so the path full path is then
# /etc/medusa-secrets/medusa_s3_credentials.
key_file = /etc/medusa-secrets/medusa_s3_credentials

[grpc]
enabled = 1
cassandra_url = http://localhost:7373/jolokia/

[logging]
level = DEBUG
```

# Deploy resources
`$ kustomize build test/config/dev/gcs | kubectl apply -f -`

or

`$ kustomize build test/config/dev/s3 | kubectl apply -f -`

Deployes the following:

* cass-operator
* medusa-operator
* Medusa configmap
* CassandraDatacenter
  * Configured with Medusa backup sidecar container
  * Configure with Medusa restore initContainer  

# Create a backup

```yaml
apiVersion: cassandra.k8ssandra.io/v1alpha1
kind: CassandraBackup
metadata:
  name: test-1
spec:
  name: test-1
  cassandraDatacenter: dc1
```

Inspect the CassandraBackup object:

```
$ kubectl -n medusa-dev get cassandrabackup test-1 -o yaml
apiVersion: cassandra.k8ssandra.io/v1alpha1
kind: CassandraBackup
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"cassandra.k8ssandra.io/v1alpha1","kind":"CassandraBackup","metadata":{"annotations":{},"name":"test-1","namespace":"medusa-dev"},"spec":{"cassandraDatacenter":"dc1","name":"test-1"}}
  creationTimestamp: "2021-01-12T15:45:19Z"
  generation: 1
  managedFields:
  - apiVersion: cassandra.k8ssandra.io/v1alpha1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          .: {}
          f:kubectl.kubernetes.io/last-applied-configuration: {}
      f:spec:
        .: {}
        f:cassandraDatacenter: {}
        f:name: {}
    manager: kubectl
    operation: Update
    time: "2021-01-12T15:45:19Z"
  - apiVersion: cassandra.k8ssandra.io/v1alpha1
    fieldsType: FieldsV1
    fieldsV1:
      f:status:
        .: {}
        f:cassdcTemplateSpec:
          .: {}
          f:spec:
            .: {}
            f:allowMultipleNodesPerWorker: {}
            f:clusterName: {}
            f:config:
              .: {}
              f:jvm-options:
                .: {}
                f:initial_heap_size: {}
                f:max_heap_size: {}
            f:managementApiAuth:
              .: {}
              f:insecure: {}
            f:podTemplateSpec:
              .: {}
              f:metadata: {}
              f:spec:
                .: {}
                f:containers: {}
                f:initContainers: {}
                f:volumes: {}
            f:resources:
              .: {}
              f:limits:
                .: {}
                f:cpu: {}
                f:memory: {}
              f:requests:
                .: {}
                f:cpu: {}
                f:memory: {}
            f:serverImage: {}
            f:serverType: {}
            f:serverVersion: {}
            f:size: {}
            f:storageConfig:
              .: {}
              f:cassandraDataVolumeClaimSpec:
                .: {}
                f:accessModes: {}
                f:resources:
                  .: {}
                  f:requests:
                    .: {}
                    f:storage: {}
                f:storageClassName: {}
        f:inProgress: {}
        f:startTime: {}
    manager: manager
    operation: Update
    time: "2021-01-12T15:45:19Z"
  name: test-1
  namespace: medusa-dev
  resourceVersion: "4288158"
  selfLink: /apis/cassandra.k8ssandra.io/v1alpha1/namespaces/medusa-dev/cassandrabackups/test-1
  uid: 96c64ac0-53e7-4e13-9867-5b11c059f57f
spec:
  cassandraDatacenter: dc1
  name: test-1
status:
  cassdcTemplateSpec:
    spec:
      allowMultipleNodesPerWorker: true
      clusterName: medusa-test
      config:
        jvm-options:
          initial_heap_size: 1024m
          max_heap_size: 1024m
      managementApiAuth:
        insecure: {}
      podTemplateSpec:
        metadata: {}
        spec:
          containers:
          - env:
            - name: JVM_EXTRA_OPTS
              value: -javaagent:/etc/cassandra/jolokia-jvm-1.6.2-agent.jar=port=7373,host=localhost
            name: cassandra
            resources: {}
            volumeMounts:
            - mountPath: /etc/cassandra
              name: cassandra-config
          - env:
            - name: MEDUSA_MODE
              value: GRPC
            image: jsanda/medusa:35d609cd0711
            imagePullPolicy: IfNotPresent
            livenessProbe:
              exec:
                command:
                - /bin/grpc_health_probe
                - -addr=:50051
              initialDelaySeconds: 10
            name: medusa
            ports:
            - containerPort: 50051
            readinessProbe:
              exec:
                command:
                - /bin/grpc_health_probe
                - -addr=:50051
              initialDelaySeconds: 5
            resources: {}
            volumeMounts:
            - mountPath: /etc/medusa/medusa.ini
              name: medusa-config
              subPath: medusa.ini
            - mountPath: /etc/cassandra
              name: cassandra-config
            - mountPath: /var/lib/cassandra
              name: server-data
            - mountPath: /etc/medusa-secrets
              name: medusa-gcp-key
          initContainers:
          - args:
            - /bin/sh
            - -c
            - wget https://search.maven.org/remotecontent?filepath=org/jolokia/jolokia-jvm/1.6.2/jolokia-jvm-1.6.2-agent.jar
              && mv jolokia-jvm-1.6.2-agent.jar /config
            image: busybox
            name: get-jolokia
            resources: {}
            volumeMounts:
            - mountPath: /config
              name: server-config
          - env:
            - name: MEDUSA_MODE
              value: RESTORE
            image: jsanda/medusa:35d609cd0711
            imagePullPolicy: IfNotPresent
            name: medusa-restore
            resources: {}
            volumeMounts:
            - mountPath: /etc/medusa/medusa.ini
              name: medusa-config
              subPath: medusa.ini
            - mountPath: /etc/cassandra
              name: server-config
            - mountPath: /var/lib/cassandra
              name: server-data
            - mountPath: /etc/medusa-secrets
              name: medusa-gcp-key
          volumes:
          - configMap:
              items:
              - key: medusa.ini
                path: medusa.ini
              name: medusa-config
            name: medusa-config
          - emptyDir: {}
            name: cassandra-config
          - name: medusa-gcp-key
            secret:
              secretName: medusa-gcp-key
      resources:
        limits:
          cpu: "1"
          memory: 2Gi
        requests:
          cpu: "1"
          memory: 2Gi
      serverImage: jsanda/mgmtapi-3_11:v0.1.13-k8c-88
      serverType: cassandra
      serverVersion: 3.11.7
      size: 3
      storageConfig:
        cassandraDataVolumeClaimSpec:
          accessModes:
          - ReadWriteOnce
          resources:
            requests:
              storage: 5Gi
          storageClassName: standard
  inProgress:
  - medusa-test-dc1-default-sts-1
  - medusa-test-dc1-default-sts-2
  - medusa-test-dc1-default-sts-0
  startTime: "2021-01-12T15:45:19Z
```

# Create the restore

```yaml
apiVersion: cassandra.k8ssandra.io/v1alpha1
kind: CassandraRestore
metadata:
  name: test
spec:
  backup: test-1
  inPlace: true
  cassandraDatacenter:
    name: dc1
    # The CRD currently requires clusterName but the operator uses reuses the cluster name
    # from the backup CassandraDatacenter
    clusterName: medusa-test
```

Inspect the CassandraRestore object:

```
$ kubectl -n medusa-dev get cassandrarestore test -o yaml
apiVersion: cassandra.k8ssandra.io/v1alpha1
kind: CassandraRestore
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"cassandra.k8ssandra.io/v1alpha1","kind":"CassandraRestore","metadata":{"annotations":{},"name":"test","namespace":"medusa-dev"},"spec":{"backup":"test-1","cassandraDatacenter":{"clusterName":"medusa-test","name":"dc1"},"inPlace":true}}
  creationTimestamp: "2021-01-12T15:50:07Z"
  generation: 1
  managedFields:
  - apiVersion: cassandra.k8ssandra.io/v1alpha1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          .: {}
          f:kubectl.kubernetes.io/last-applied-configuration: {}
      f:spec:
        .: {}
        f:backup: {}
        f:cassandraDatacenter:
          .: {}
          f:clusterName: {}
          f:name: {}
        f:inPlace: {}
    manager: kubectl
    operation: Update
    time: "2021-01-12T15:50:07Z"
  - apiVersion: cassandra.k8ssandra.io/v1alpha1
    fieldsType: FieldsV1
    fieldsV1:
      f:status:
        .: {}
        f:finishTime: {}
        f:restoreKey: {}
        f:startTime: {}
    manager: manager
    operation: Update
    time: "2021-01-12T15:50:07Z"
  name: test
  namespace: medusa-dev
  resourceVersion: "4290008"
  selfLink: /apis/cassandra.k8ssandra.io/v1alpha1/namespaces/medusa-dev/cassandrarestores/test
  uid: bfe7c5e2-94c7-4d4f-aab1-3e480bf245c8
spec:
  backup: test-1
  cassandraDatacenter:
    clusterName: medusa-test
    name: dc1
  inPlace: true
status:
  finishTime: "2021-01-12T15:50:07Z"
  restoreKey: 22fa5199-b0d6-4643-9b9b-ac025c575b8c
  startTime: "2021-01-12T15:50:07Z"
```