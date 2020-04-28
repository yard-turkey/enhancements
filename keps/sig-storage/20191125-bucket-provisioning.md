---
title: Object Bucket Provisioning
authors:
  - "@jeffvance"
  - "@copejon"
owning-sig: "sig-storage"
reviewers:
  - "@saad-ali"
  - "@alarge"
  - "@erinboyd"
  - "@guymguym"
  - "@travisn"
approvers:
  - TBD
editor: TBD
creation-date: 2019-11-25
last-updated: 2020-04-28
status: provisional
---

# Object Bucket Provisioning

## Table of Contents

<!-- toc -->
- [Summary](#summary)
  - [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
  - [Vocabulary](#vocabulary)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
      - [Admin](#admin)
      - [User](#user)
  - [API Relationships](#api-relationships)
  - [Custom Resource Definitions](#custom-resource-definitions)
      - [Bucket](#bucket)
      - [BucketContent](#bucketcontent)
      - [BucketClass](#bucketclass)
<!-- /toc -->

# Summary

This proposal introduces the Container Object Storage Interface (COSI), whose purpose is to provide a  standardized representation of object storage instances in Kubernetes with support for the most common object store interfaces.  Given a common interface, cluster workloads can be made COSI-aware, and able to ingest buckets via the Kubernetes control layer.  While absolute portability cannot be guaranteed because of incompatibilities between providers, workloads reliant on a given protocol (e.g. one of S3, GCS, Azure Blob) may be defined in a single manifest and deployed wherever that protocol is supported. 

The COSI API wil also provide a path towards a community maintained COSI controller, which will be capable of bucket lifecycle operations.  It is anticipated that the controller provide an API such that pluggable drivers may be written to implement operations specific to the object store provider.  

This proposal does _not_ include a standardized *protocol* or abstraction of storage vendor APIs.  

## Motivation

File and block are first class citizens within the Kubernetes ecosystem.  Object, though different on a fundamental level, is a popular means of storing data, especially against very large data sources.   As such, we feel it is in the interest of the community to elevate buckets to a community supported feature.  In doing so, we can provide Kubernetes cluster users and administrators a normalized and familiar means of managing object storage.

## Goals
+ Define a control plane API in order to standardize and formalize Kubernetes object storage representation.
+ As MVP, be accessible to the largest groups of consumers by supporting the major object storages protocols (S3, Google Cloud Storage, Azure Blob) while being extensible for future protocol additions.
+ Present similar workflows for both new-bucket and imported bucket operations.
+ Use standard Kubernetes mechanisms to sync a pod with the readiness of the bucket it will consume. This can be accomplished via Secrets.

## Non-Goals

+ Define a native _data-plane_ object store API which would greatly improve object store app portability.
+ Mirror the static workflow of PersistentVolumes wherein users are given the first available Volume.  Pre-provisioned buckets are expected to be non-empty and thus unique.
+ Strictly define automation around the COSI API.

##  Vocabulary

+  _Brownfield Bucket_ - externally created and represented by a `BucketClass` and managed by a provisioner.
+ _Bucket_ - A user-namespaced custom resource representing an object store bucket.
+  _BucketClass_ - A cluster-scoped custom resource containing fields defining the provisioner and an immutable parameter set.
   + _In Greenfield_: an abstraction of new bucket provisioning.
   + _In Brownfield_: an abstration of an existing objet store bucket.
+ _BucketContent_ - A cluster-scoped custom resource bound to a `Bucket` and containing relevant metadata.
+ _Greenfield Bucket_ - a new bucket created and managed by the COSI system.
+  _Object_ - An atomic, immutable unit of data stored in buckets.
+ _Driverless Bucket_ - externally created and manually integrated bucket with no installed provisioner.

# Proposal

## User Stories

#### Admin

- As a cluster administrator, I can set quotas and resource limits on generated buckets' storage capacity via the Kubernete's API, so that  I can control monthly infrastructure costs.
- As a cluster administrator, I can use Kubernetes RBAC policies on bucket APIs, so that I may control integration and access to pre-existing buckets from within the cluster, reducing the need to administer an external storage interface.
- As a cluster administrator, I can manage multiple object store providers via the Kubernetes API, so that I do not have to become an expert in several different storage interfaces.

#### User

- As a developer, I can define my object storage needs in the same manifest as my workload, so that deployments are streamlined and encapsulated within the Kubernetes interface.
- As a developer, I can define a manifest containing my workload and object storage configuration once, so that my app may be ported between clusters as long as the storage provided supports my designated data path protocol.

## API Relationships

The diagram below indicates the relationships by reference between the proposed APIs, the user facing Kubernetes primitives, and the actual storage and identity instances.  COSI APIs (light green) bridge the gap between workloads and object stores, providing a standardized means of consuming object storage for Kubernetes operators and workloads.



![](./bucket-api-relationships.png)

## Custom Resource Definitions

#### Bucket

A user facing API object representing an object store bucket. Created by a user in their app's namespace. Once provisiong is complete, the `Bucket` is "bound" to the corresponding `BucketContent`. Binding is 1:1, meaning there is only one `BucketContent` per `Bucket` and vice-versa.


```yaml
apiVersion: cosi.io/v1alpha1
kind: Bucket
metadata:
  name:
  namespace:
  labels:
    cosi.io/provisioner: [1]
  finalizers:
  - cosi.io/finalizer [2]
spec:
  protocol:
    type: ""
    s3:
      accessKeyId:
      userName:
    gcs:
      serviceAccount:
      privateKeyName:
    azure:
      storageAccountName:
  bucketPrefix: [4]
  bucketClassName: [5]
  secretName: [6]
status:
  bucketContentName: [7]
  phase: [8]
  conditions: 
```
1. `labels`: COSI controller adds the label to its managed resources to easy CLI GET ops.  Value is the driver name returned by GetDriverInfo() rpc. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
1. `finalizers`: COSI controller adds the finalizer to defer `Bucket` deletion until backend deletion ops succeed.
1. `requestProtocol`: specifies the desired data format in which credentials are provided to the app.  One of {“s3”, “gcs”, or “azureBlob”}. 
1. `bucketPrefix`: (Optional) prefix prepended to a randomly generated bucket name, eg. "YosemitePhotos-". If empty no prefix is appended.
1. `bucketClassName`: Name of the target `BucketClass`.
1. `secretName`: Desired name for user's credential Secret. Defining this name allows for a single manifest workflow.  In cases of name collisions, attempting to create the user's secret will continue until a timeout occurs.
1. `bucketContentName`: Name of a bound `BucketContent`.
1. `phase`: 
   - *Pending*: The controller has detected the new `Bucket` and begun provisioning operations
   - *Bound*: Provisioning operations have completed and the `Bucket` has been bound to a `BucketContent`.

#### BucketContent

A cluster-scoped resource representing an object store bucket. The `BucketContent` is expected to store stateful data relevant to bucket deprovisioning. The `BucketContent` is bound to the `Bucket` in a 1:1 mapping. For MVP a `BucketContent` is not reused.

```yaml
apiVersion: cosi.io/v1alpha1
kind: BucketContent
Metadata:
  name: [1]
  labels:
    cosi.io/provisioner: [2]
  finalizers:
  - cosi.io/finalizer [3]
spec:
  provisioner: [4]
  releasePolicy: [5]
  accessMode: [6]
  bucketClassName: [7]
  bucketRef: [8]
    name:
    namespace:
    uuid:
    resourceVersion:
  secretRef: [9]
    name:
    namespace:
  protocol: [10]
    type: ""
    azureBlob: [11]
      storageAccountName:
      accountKey:
      containerName:
    s3: [12]
      endpoint:
      accessKeyId:
      bucketName:
      region:
      signatureVersion:
      userName:
    gcs: [13]
      bucketName:
      privateKeyName:
      projectId:
      serviceAccount:
  parameters: [14]
status:
  message: [15]
  phase: [16]
  conditions:
```
1. `name`: Generated in the pattern of `<BUCKET-CLASS-NAME>'-'<RANDOM-SUFFIX>`. 
1. `labels`: COSI controller adds the label to its managed resources for easy CLI GET ops.  Value is the driver name returned by GetDriverInfo() rpc. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
1. `finalizers`: COSI controller adds the finalizer to defer Bucket deletion until backend deletion ops succeed.
1. `provisioner`: The provisioner field defined in the BucketClass.  Used by sidecars to filter BucketContents.
1. `releasePolicy`: Prescribes outcome of a Delete events. **Note:** In Brownfield and Static cases, *Retain* is mandated.
    - _Delete_:  the bucket and its contents are destroyed
    - _Retain_:  the bucket and its data are preserved with only abstracting Kubernetes being destroyed
1. `accessMode`: Declares the level of access given to credentials provisioned through this class.     If empty, drivers may set defaults.
1. `bucketClassName`: Name of the associated `BucketClass`.
1. `bucketRef`: the name & namespace of the associated `Bucket`.
   
    - `name` : the Bucket’s name
    - `namespace`: the Bucket’s namespace
    - `uuid`: the Bucket’s API server generated UUID
    - `resourceVersion`: the Bucket's resourceVersion
1. `secretRef`: the name and namespace of the source secret.  This `secret` is either generated by the driver or created by an admin.
1. `protocolAzureBlob`: data required to target a provisioned azure container and/or storage account
1. `protocol`: the protocol specified by the `Bucket`.
1. `protocolS3`: data required to target a provisioned S3 bucket and/or user
1. `protocolGcs`: data required to target a provisioned GCS bucket and/or service account
1. `parameters`: a copy of the BucketClass parameters
1. `message`: a human readable description detailing the reason for the current `phase``
1. `phase`: is the current state of the `BucketContent`:
     - _Bound_: the controller finished processing the request and bound the `Bucket` and `BucketContent`
     - _Released_: the `Bucket` has been deleted, signalling that the `BucketContent` is ready for garbage collection.
     - _Failed_: error and all retries have been exhausted.
     - _Retrying_: set when a driver or Kubernetes error is encountered during provisioning operations indicating a retry loop.

#### BucketClass

A cluster-scoped custom resource used to describe both greenfield and brownfield buckets.  The `BucketClass` defines a release policy, and specifies driver specific parameters and the provisioner name. The `provisioner` value is used by sidecars to filter `BucketContent` objects.

There is currently no default bucket class.

```yaml
apiVersion: cosi.io/v1alpha1
kind: BucketClass
metadata:
  name: 
provisioner: [1]
supportedProtocols: {"azureblob", "gcs", "s3", ... } [2]
accessMode: {"ro", "wo", "rw"} [3]
releasePolicy: {"Delete", "Retain"} [4]
bucketContentRef: [5]
  name:
  uuid:
secretRef: [6]
  name:
  namespace:
parameters: [7]
isDefaultBucketClass: [8]
```

1. `provisioner`: The name of the driver. If supplied the driver container and sidecar container are expected to be deployed. If omitted the `secretRef` is required for static provisioning.
1. `supportedProtocols`: protocols the associated object store supports.  Applied when matching Bucket to BucketClasses.
1. `accessMode`: (Optional) Declares the level of access given to credentials provisioned through this class.     If empty, defaults to `rw`.
1. `releasePolicy`: Prescribes outcome of a Delete events. **Note:** In Brownfield and Static cases, *Retain* is mandated. 
    - `Delete`:  the bucket and its contents are destroyed
    - `Retain`:  the bucket and its data are preserved with only abstracting Kubernetes being destroyed
1. `bucketContentRef:` (Optional) When specified, indicates a single `BucketConetent` for brownfield or static operations.
1. `secretRef`: (Optional) The name and namespace of an existing secret to be copied to the `Bucket`'s namespace for static provisioning.  Requires that `bucketContentRef` point to an existing `BucketContent` . Used for brownfield and static cases.
1. `parameters`: (Optional) Object store specific string:string map passed to the driver.
1. `isDefaultBucketClass`: boolean. When true, signals that the COSI controller should attempt to match `Bucket`’s without a defined `BucketClass` to this class, accounting for the `Bucket`’s requested protocol.  Multiple default classes for the same protocol will produce undefined behaviour, likely matching the first default class that is found.
