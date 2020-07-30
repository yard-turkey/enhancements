---

Object Bucket Provisioning

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
last-updated: 2020-07-08
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
  - [APIs](#apis)
    - [Storage APIs](#storage-apis)
      - [BucketRequest](#bucketrequest)
      - [Bucket](#bucket)
      - [BucketClass](#bucketclass)
    - [Access APIs](#access-apis)
      - [BucketAccessRequest](#bucketaccessrequest)
      - [BucketAccess](#bucketaccess)
      - [BucketAccessClass](#bucketaccessclass)
    - [App Pod](#app-pod)
  - [Topology](#topology)
  - [Workflows](#workflows)
    - [Create](#create)
    - [Delete](#delete)
  - [Provisioner Secrets](#provisioner-secrets)
  - [gRPC Definitions](#grpc_definitions)
<!-- /toc -->
# Summary

This proposal introduces the *Container Object Storage Interface* (COSI), a system composed of Custom Resource Definitions (CRDs), a controller architecture, and a gRPC specification, for the purpose of standardizing object storage representations in Kubernetes.  Goals and non-goals set the scope of the proposal by defining higher level objectives.  The vocabulary section defines terminology.  User stories illustrate how these APIs fulfill user and admin requirements.  Relationships between the APIs are provided to illustrate the interconnections between object storage APIs, users' workloads, and object store service instances.  Lastly, the documents states the proposed API specs for the BucketRequest, Bucket, BucketClass, and various access related objects, create and delete workflows, and the full gRPC spec.

## Motivation

File and block are first class citizens within the Kubernetes ecosystem.  Object, though very different under the hood, is a popular means of storing data, especially against very large data sources.   As such, we feel it is in the interest of the community to integrate object storage into Kubernetes, supported by the SIG-Storage community.  In doing so, we can provide Kubernetes cluster users and administrators a normalized and familiar means of managing object storage. 

While absolute portability cannot be guaranteed because of incompatibilities between providers, workloads reliant on a given protocol (e.g. one of S3, GCS, Azure Blob) may be defined in a single manifest and deployed wherever that protocol is supported.

This proposal does _not_ include a standardized *protocol* or abstraction of storage vendor APIs

## Goals

+ Specify object storage Kubernetes APIs for the purpose of orchestrating object store operations
+ Implement a Kubernetes controller architecture with support for pluggable provisioners
+ As MVP, be accessible to the largest groups of consumers by supporting the major object storage protocols (S3, Google Cloud Storage, Azure Blob) while being extensible for future protocol additions.
+ Present similar workflows for both greenfield and brownfield bucket operations.

## Non-Goals

+ Define the _data-plane_ object store interface to replace or supplement existing vendor interfaces (i.e. replace GCS, S3, or Azure Blob)
+ Bucket access management is not within the scope of this KEP.  ACLs, access policies, and credentialing need to be handled out-of-band.

##  Vocabulary

+ _brownfield bucket_ - an existing storage bucket which could be part of a Kubernetes cluster or completely separate.
+ _BucketRequest_ - a user-namespaced custom resource representing a request for a storage instance endpoint.
+ _BucketClass_ - a cluster-scoped custom resource containing fields defining the provisioner and an immutable parameter set for creating new buckets.
+ _Bucket_ - a cluster-scoped custom resource referenced by a `BucketRequest` and containing connection information and metadata for a storage instance.
+ _cosi-node-adapter_ - a pod per node which receives Kubelet gRPC _NodePublishVolume_ and _NodeUnpublishVolume_ requests, ensures the target bucket has been provisioned, and notifies the kubelet when the pod can be run.
+ _driver_ - a container (usually paired with a sidecar container) that is reponsible for communicating with the underlying object store to create, delete, grant access to, and revoke access from buckets. Drivers talk gRPC and need no knowledge of Kubernetes. Drivers are typically written by storage vendors, and should not be given any access outside of their namespace.
+ _greenfield bucket_ - a new bucket created by automation.
+ _object_ - an atomic, immutable unit of data stored in buckets.
+ _provisioner_ - a generic term meant to describe the combination of a sidecar and driver. "Provisioning" a bucket can mean creating a new bucket or granting access to an existing bucket.
+ _sidecar_ - a Kubernetes-aware container (usually paired with a driver) that is reponsible for fullfilling COSI requests with the driver via gRPC calls. The sidecar's access can be restricted to the namespace where it resides, which is expected to be the same namespace as the provisioner. The COSI sidecar is provided by the Kubernetes community.
+ _storage instance_ - refers to the back object storage endpoint being abstracted by the Bucket API (a.k.a “bucket” or “container”).
+ _driverless_ - a system where no driver is deployed to automate object store operations.  COSI automation may still be deployed to managed COSI APIs. **Note:** the current state of the KEP does not justify nor define driverless aka "static provisioning". If the community feels this is an MVP requirement we will need to flush this out.

# Proposal

## User Stories

#### Admin

- As a cluster administrator, I can control access to new and existing buckets when accessed from the cluster, regardless of the backing object store.

#### User

- As a developer, I can define my object storage needs in the same manifest as my workload, so that deployments are streamlined and encapsulated within the Kubernetes interface.
- As a developer, I can define a manifest containing my workload and object storage configuration once, so that my app may be ported between clusters as long as the storage provided supports my designated data path protocol.
- As a developer, I want to create a workload controller which is bucket API aware, so that it can dynamically connect workloads to object storage instances.

## APIs

### Storage APIs

#### BucketRequest

A user facing, namespaced custom resource requesting a bucket endpoint. A `BucketRequest` (BR) lives in the app's namespace.  In addition to a `BucketRequest`, a [BucketAccessRequest](#bucketaccessrequest) is required in order to grant credentialed access to the bucket. 

**NOTE:** _Currently under review: we are discussing whether or not a BR is needed for any brownfield use-cases. We are considering BRs only applies to greenfield bucket creation (where a BAR is also needed), and that all brownfield accesses need only a BAR._

```yaml
apiVersion: cosi.io/v1alpha1
kind: BucketRequest
metadata:
  name:
  namespace:
  labels:
    cosi.io/provisioner: [1]
  finalizers:
  - cosi.io/finalizer [2]
spec:
  protocol: [3]
  bucketPrefix: [4]
  bucketClassName: [5]
  bucketInstanceName: [6]
status:
  phase: [7]
  conditions:
```

1. `labels`: added by the controller.  Key’s value should be the provisioner name. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
1. `finalizers`: added by controller to defer `BucketRequest` deletion until backend deletion ops succeed.
1. `protocol`: (required) specifies the desired protocol.  One of {“s3”, “gcs”, or “azureBlob”}.
1. `bucketPrefix`: (optional) prefix prepended to a randomly generated bucket name, eg. “yosemite-photos-". If empty, no prefix is prepended. If `bucketInstanceName` is also supplied then it overrides `bucketPrefix'.
1. `bucketClassName`: (optional) name of the target `BucketClass` used for greenfield provisioning only. If omitted, a default bucket class matching the protocol is searched for. If the bucket class does not support the requested protocol, an error is logged and retries occur.
1. `bucketInstanceName`: (optional) name of the cluster-wide `Bucket` instance. If blank, then COSI fills in the name during the binding step. If defined by the user, then this names the `Bucket` instance created by the admin. There is no automation available to make this name known to the user. Once a `Bucket` instance is created, the name of the actual bucket can be found.
1. `phase`: 
   - *Creating*: COSI is in the process of provisioning the bucket, meaning creating a new bucket or granting access to an existing bucket.
   - *Created*: COSI has provisioned the bucket, meaning a new bucket has been created (if greenfield) and access to the bucket has been granted.
   - *Bound*: access to a bucket has been granted, and, for greenfield, a new bucket was created. The `Bucket` is bound to a `BucketAccess` instance.
   - *Released*: the Bucket is unbound and can potentially reused.
   - *Deleted*: the physical bucket has been deleted and the `Bucket` is about to be removed.

> Note: additionally there are some error phases, such as *ErrBucketClassDoesNotSupportProtocol*, *ErrBucketDeletionInProgress*, etc.

#### Bucket

A cluster-scoped custom resource representing the abstraction of a single object store bucket. At a minimum, a `Bucket` instance stores enough identifying information so that drivers can accurately target the storage instance (e.g. during a deletion process).  For greenfield `Bucket`s all of the associated bucket classes fields are copied to the `Bucket`. Additionally, endpoint data returned by the driver is copied to the `Bucket` by the sidecar.

There is a 1-to-many relationship between a `Bucket` and a `BucketRequest`, meaning that many `BucketRequest`s can reference the same `Bucket`.

For greenfield, COSI creates the `Bucket` based on values in the `BucketRequest` and `BucketClass`. For brownfield, an admin manually creates the `Bucket` and COSI takes care of binding and populating fields returned by the provisioner.

```yaml
apiVersion: cosi.io/v1alpha1
kind: Bucket
metadata:
  name: [1]
  labels:
    cosi.io/provisioner: [2]
  finalizers:
    - cosi.io/finalizer [3]
spec:
  provisioner: [4]
  releasePolicy: [5]
  anonymousAccessMode: [6]
    - private
    - publicRead
    - publicReadWrite
  bucketClassName: [7]
  bindings: [8]
    - "<BucketAccess.name>"
  protocol: [9]
    protocolSignature: ""
    azureBlob:
      containerName:
      storageAccount:
    s3:
      endpoint:
      bucketName:
      region:
      signatureVersion:
    gcs:
      bucketName:
      privateKeyName:
      projectId:
      serviceAccount:
  parameters: [10]
status:
  phase: [11]
  conditions:
```

1. `name`: When created by COSI, the `Bucket` name is generated in this format: _"bucket-"<bucketRequest.name>"-"<bucketRequest.namespace>_. If an admin creates a `Bucket`, as is necessary for brownfield access, they can use any name.
2. `labels`: added by the controller.  Key’s value should be the provisioner name. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
3. `finalizers`: added by the controller to defer `Bucket` deletion until backend deletion ops succeed.
4. `provisioner`: The provisioner field as defined in the `BucketClass`.  Used by sidecars to filter `Bucket`s. Format: <provisioner-namespace>"/"<provisioner-name>, eg "ceph-rgw-provisoning/ceph-rgw.cosi.ceph.com".
5. `releasePolicy`: Prescribes outcome of a Delete events. 
   - _Retain_: (default) the `Bucket` and its data are preserved. The `Bucket` can potentially be reused.
   - _Delete_: the bucket and its contents are destroyed.
> Note: the `Bucket`'s release policy is set to "Retain" as a default. Exercise caution when using the "Delete" release policy as the bucket content will be deleted.
> Note: a `Bucket` is not deleted if it is bound to any `BucketRequest`s.
6. `anonymousAccessMode`:  ACL specifying *uncredentialed* access to the Bucket.  This is applicable to cases where the storage instance or objects are intended to be publicly readable and/or writable.  Accepted values:
   - `private`: Default, disallow uncredentialed access to the storage instance.
   - `ro`: Read only, uncredentialed users can call ListBucket and GetObject.
   - `rw`: Read/Write, same as `ro` with the addition of PutObject being allowed.
   - `wo`: Write only, uncredentialed users can only call PutObject.
> Note: does not reflect or alter the backing storage instances' ACLs or IAM policies.
7. `bucketClassName`: Name of the associated bucket class (greenfield only).
8. `bindings`: an array of `BucketAccess.name`(s). If the list is empty then there are no bindings (accessors) to this `Bucket` instance and the `Bucket` can potentially be deleted.
10. `protocol`: The protocol the application will use to access the storage instance.
   - `protocolSignature`: Specifies the protocol targeted by this Bucket instance.  One of:
     - `azureBlob`: data required to target a provisioned azure container and/or storage account.
     - `s3`: data required to target a provisioned S3 bucket and/or user.
     - `gcs`: data required to target a provisioned GCS bucket and/or service account.
11. `parameters`: a copy of the BucketClass parameters.
12. `phase`: is the current state of the `Bucket`:
   - *Creating*: the controller is in the process of provisioning the bucket, meaning creating a new bucket or granting access to an existing bucket.
   - *Bound*: access to a bucket has been granted, and, for greenfield, a new bucket was created. The `Bucket` is bound to a `BucketAccess`.
   - *Released*: the Bucket is unbound and can potentially reused.
   - *Deleted*: the physical bucket has been deleted and the `Bucket` is about to be removed.

#### BucketClass

A cluster-scoped custom resource to provide admins control over the handling of greenfield bucket provisioning.  The `BucketClass` (BC) defines a release policy, specifies driver specific parameters, and provides the provisioner name. A default bucket class can be defined for each supported protocol, which allows the bucket class to be omitted from a `BucketRequest`. Most of the `BucketClass` fields are copied to the generated (greenfield) `Bucket` instance.

> Note: Bucket classes are not used for brownfield provisioning. Instead, an admin manually creates a `Bucket` and the user references this `Bucket` in their `BucketRequest.bucketInstanceName`.

```yaml
apiVersion: cosi.io/v1alpha1
kind: BucketClass
metadata:
  name: 
provisioner: [1]
isDefaultBucketClass: [2]
protocol: {"azureblob", "gcs", "s3", ... } [3]
anonymousAccessMode: {"ro", "wo", "rw"} [4]
releasePolicy: {"Delete", "Retain"} [5]
parameters: [6]
```

1. `provisioner`: the name of the driver. If supplied the driver container and sidecar container are expected to be deployed. Format: <provisioner-namespace>"/"<provisioner-name>, eg "ceph-rgw-provisoning/ceph-rgw.cosi.ceph.com".
2. `isDefaultBucketClass`: (optional) boolean, default is false. If set to true then potentially a `BucketRequest` does not need to specify a `BucketClass`. If the greenfield `BucketRequest` omits the `BucketClass` and a default `BucketClass`'s protocol matches the `BucketRequest`'s protocol then the default bucket class is used.
3. `protocol`: (required) protocol supported by the associated object store. This field validates that the `BucketRequest`'s desired protocol is supported.
> Note: if an object store supports more than one protocol then the admin should create a `BucketClass` per protocol.
4. `anonymousAccessMode`: (optional) ACL specifying *uncredentialed* access to the Bucket.  This is applicable for cases where the storage instance or objects are intended to be publicly readable and/or writable.
6. `releasePolicy`: defines bucket retention for greenfield `BucketRequest` deletes. **
   - _Retain_: (default) the `Bucket` and its data are preserved. The `Bucket` can potentially be reused.
   - _Delete_: the bucket and its contents are destroyed.
> Note: the `Bucket`'s release policy is set to "Retain" as a default. Exercise caution when using the "Delete" release policy as the bucket content will be deleted.
> Note: a `Bucket` is not deleted if it is bound to any `BucketRequest`s.
7. `parameters`: (optional) a map of string:string key values.  Allows admins to set provisioner key-values.  **Note:** see [Provisioner Secrets](#provisioner-secrets) for some predefined `parameters` settings.

### Access APIs

The Access APIs abstract the backend policy system.  Access policy and user identities are an integral part of most object stores.  As such, a system must be implemented to manage both user/credential creation and the binding of those users to individual buckets via policies.  Object stores differ from file and block storage in how they manage users, with cloud providers typically integrating with an IAM platform.  This API includes support for cloud platform identity integration with Kubernetes ServiceAccounts.  On-prem solutions usually provide their own user management systems, which may look very different from each other and from IAM platforms.  We must also account for third party authentication solutions that may be integrated with an on-prem service.

#### BucketAccessRequest

A user namespaced custom resource representing an object store user and an access policy defining the user’s relation to a storage instance.  A user creates a `BucketAccessRequest` (BAR) in the app's namespace (which is the same namespace as the `BucketRequest`).  A 'BucketAccessRequest' can specify *either* a ServiceAccount or a desired Secret name.  Specifying a ServiceAccount enables provisioners to support cloud provider identity integration with their respective Kubernetes offerings.

**NOTE:** _Currently under review: we are discussing whether or not a BR is needed for any brownfield use-cases. We are considering BRs only applies to greenfield bucket creation (where a BAR is also needed), and that all brownfield accesses need only a BAR._

```yaml
apiVersion: cosi.io/v1alpha1
kind: BucketAccessRequest
metadata:
  name:
  namespace:
  labels:
    cosi.io/provisioner: [1]
  finalizers:
  - cosi.io/finalizer [2]
spec:
  serviceAccountName: [3]
  accessSecretName: [4]
  bucketInstanceName: [5]
  bucketAccessClassName: [6]
  bucketAccessName: [7]
status:
  phase: [8]
```

1. `labels`: added by the controller.  Key’s value should be the provisioner name. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
1. `finalizers`: added by the controller to defer `BucketAccessRequest` deletion until backend deletion ops succeed.
1. `serviceAccountName`: (optional) the name of a Kubernetes ServiceAccount in the same namespace.  This field is included to support cloud provider identity integration.  Should not be set when specifying `accessSecretName`.
1. `accessSecretName`: (optional) the name of a Kubernetes Secret in the same namespace.  This field is used when there is not cloud provider identity integration.  Should not be set when specifying `serviceAccountName`.
1. `bucketInstanceName`: The the name of the `Bucket` instance to which the user identity or ServiceAccount should be granted access to, according to the policies defined in the `BucketAccessClass`.
1. `bucketAccessClassName`: name of the `BucketAccessClass` specifying the desired set of policy actions to be set for a user identity or ServiceAccount.
1. `bucketAccessName`: name of the bound cluster-scoped `BucketAccess` instance.
1. `phase`: is the current state of the `BucketAccessRequest`:
   - *Pending*: The controller has detected the new `BucketAccessRequest` and begun provisioning operations.
   - *Bound*: Provisioning operations have completed and the `BucketAccessRequest` has been bound to a `BucketAccess`. It also indicated that the `BucketAccess` instance has been bound to the `Bucket`.
   - *Released*: the Bucket is unbound and can potentially reused.

#### BucketAccess

A cluster-scoped administrative custom resource which encapsulates fields from the `BucketAccessRequest` (BAR) and the `BucketAccessClass` (BAC).  The purpose of the `BucketAccess` (BA) is to serve as communication path between provisioners and the central COSI controller.  In greenfield, the COSI controller creates `BucketAccess` instances for new `BucketAccessRequest`s.  There is one `BucketAccess` instance per `BucketAccessRequest`.

```yaml
apiVersion: cosi.io/v1alpha1
kind: BucketAccess
metadata: 
  name: [1]
  labels:
    cosi.io/provisioner: [2]
  finalizers:
  - cosi.io/finalizer [3]
 spec:
  bucketAccessRequestName: [4]
  bucketAccessRequestNamespace:
  serviceAccountName: [5]
  accessSecretName: [6]
  provisioner: [7]
  parameters: [8]
 status:
  phase: [9]
```
**NOTE:** _if we decide to not require a BR for brownfield bucket access, then the BAR will need `protocol` added_if we decide to not require a BR for brownfield bucket access, then the BAR will need `protocol` added_.

1. `name`: For greenfield, generated in the pattern of `"bucketAccess-"<bucketAccessRequest.name>"-"<bucketAccessRequest.namespace>`. 
1. `labels`: added by the controller.  Key’s value should be the provisioner name. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
1. `finalizers`: added by the controller to defer `BucketAccess` deletion until backend deletion ops succeed.
1. `bucketAccessRequestName`/`bucketAccessRequestNamespace`: name and namespace of the bound `BucketAccessRequest`
1. `serviceAccountName`: name of the Kubernetes ServiceAccount specified by the `BucketAccessRequest`.  Undefined when the `BucketAccessRequest.accessSecretName` is defined.
1. `  accessSecretName`: name of the provisioner generated Secret containing access credentials. This Secret exists in the provisioner’s namespace and must be copied to the app namespace by the COSI controller.
1. `provisioner`:  name of the provisioner that should handle this `BucketAccess` instance.  Copied from the `BucketAccessClass`.
1. `parameters`:  A map of string:string key values.  Allows admins to control user and access provisioning by setting provisioner key-values.
1. `phase`: is the current state of the `BucketAccess`:
   - *Pending*: The controller has detected the new `BucketAccessRequest` and begun provisioning operations.
   - _Bound_: the controller finished processing the request and has bound the `BucketAccess` to the both the `BucketAccessRequest` and to the `Bucket`.
   - _Released_: the originating Bucket has been deleted, signalling that the Bucket is ready for garbage collection.  This will occur on greenfield Buckets once all requests referencing the Bucket are deleted.

#### BucketAccessClass

A cluster-scoped custom resource providing a way for admins to specify policies that may be used to access buckets.  A `BucketAccessClass` (BAC) is always applicable in greenfield, where access is dynamically granted, and only sometimes applicable in brownfield, where a user's identity may already exist, but not yet have access to a bucket.  In this case, a `BucketAccessRequest` will still specify the `BucketAccessClass` in order to determine which actions it is granted.

```yaml
apiVersion: cosi.io/v1alpha1
kind: BucketAccessClass
metadata: 
  name:
provisioner: [1]
policyActions: [2]
  allow:
  - "*"
  deny:
  - "*"
parameters: [3]
```

1. `provisioner`: (required) the name of the driver that `BucketAccess` instances should be managed by. Format: <provisioner-namespace>"/"<provisioner-name>, eg "ceph-rgw-provisoning/ceph-rgw.cosi.ceph.com".
1. `policyActions`: a set of provisioner/platform defined policy actions to allow or deny a given user identity.
1. `parameters`: (Optional)  A map of string:string key values.  Allows admins to control user and access provisioning by setting provisioner key-values.

---

### App Pod
The application pod utilizes CSI's inline empheral volume support to provide the endpoint and secret credentials in an in-memory volume. This approach also, importantly, prevents the pod from launching before the bucket has been provisioned since the kubelet waits to start the pod until it has received the cosi-node-adpater's `NodePublishVolume` response.

Here is a sample pod manifest:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app-pod
  namespace: dev-ns
spec:
  serviceAccountName: [1]
  containers:
    - name: my-app
      image: ...
      volumeMounts:
        - mountPath: /cosi [2]
          name: cosi-vol
  volumes:
    - name: cosi-vol
      csi: [3]
        driver: cosi.sigs.k8s.io [4]
        volumeAttributes: [5]
          bucketRequestName: <my-br-name>
          bucketAccessRequestName: <my-bar-name>
          bucketRequestNamespace:
```
1. the service account may be needed depending on cloud IAM intergration with kubernetes.
1. the mount path is the directory where the app will read the credentials and endpoint.
1. this is an inline CSI volume.
1. the name of the cosi-node-adapter.
1. information needed by the cosi-node-adapter to verify that the bucket has been provisioned.

> Note: `VolumeLifeCycleModes` needs to be set to "empheral" for inline COSI node adapter.

### Topology

![Architecture Diagram](COSI%20Architecture_COSI%20architecture.png)

## Workflows
Here we describe the workflows used to create/provision new or existing buckets and to delete/de-provision buckets.

>  Note: Per [Non-Goals](#non-goals), access management is not within the scope of this KEP.  ACLs, access policies, and credentialing should be handled out of band.

### CreateBucket

![CreateBucket Workflow](COSI%20Architecture_Create%20Bucket%20Workflow.png)

_Create_ covers creating a new bucket and/or granting access to an existing bucket. In both cases the `Bucket` and `BucketAccess` resources described above are instantiated.

Also, when the app pod is created, the kubelet will gRPC call `NodePublishVolume` which is received by the cosi-node-adapter. The pod hangs until the adapter responds to the gRPC request. The adapter ensures that the target bucket has been provisioned and is ready to be accessed.

"Greenfield" defines a new bucket created based on a user's [BucketRequest](#bucketrequest), and access granted to this bucket.
“Brownfield” describes any case where access needs to be granted to an existing bucket. In brownfield, this bucket is abstracted by a [Bucket](#Bucket) instance, and expected to be created manually by the admin. There can be multiple `BucketRequests` that bind to a single `Bucket`.

> Note: COSI determines that a request is for a new bucket when an associated `Bucket` instance does not exist.  Consider that `BucketRequest.bucketClassName` can be blank because COSI supports default bucket classes. Also, `BucketRequest.bucketInstanceName` is filled in for brownfield.

Prep for brownfield:
+ admin creates a `Bucket` with most of the fields filled in.
+ note that a `BucketClass` is not used for brownfield.

Here is the workflow:

**NOTE:** _Currently under review: we are discussing whether or not a BR is needed for any brownfield use-cases. We are considering BRs only applies to greenfield bucket creation (where a BAR is also needed), and that all brownfield accesses need only a BAR. If this is accepted then the workflows will be updated._

+ COSI central controller detects a new `BucketRequest` (BR).
+ COSI central controller detects a new `BucketAccessRequest`(BAR).
+ COSI gets the `BR.BucketClass` (directly or via the matching default).
+ COSI gets the `BAR.bucketAccessClass` object.
+ COSI creates the associated `Bucket`, after filling in a template consisting of fields from the `BucketClass` and BR.
  + If this create operation fails due to the `Bucket` already existing, then we have a brownfield case; otherwise it's greenfield.  **Note:** the goal here is to reduce separate greenfield vs. brownfield logic in the code.
+ COSI creates the associated `BucketAccess` (BA), after filling in a template consisting of fields from the `BucketAccessClass` and BAR.
+ For a newly created `Bucket`, the sidecar sees it and gRPC calls the driver to create a bucket.
+ COSI is done with the `BucketRequest`, but there is no binding until the `BucketAccess` instance is created for the associated `BucketAccessRequest`.
+ The sidecar sees the newly created BA and gRPC calls the driver to grant access.
+ COSI sees the completed BA which triggers binding the BA in `Bucket`.

+ Depending on when the app pod was started, the kubelet call `NodePublishVolume` and waits for the response from the cosi-node-adapter.
+ The cois-node-adaper sees the `NodePublishVolume` request and is passed the `BucketRequest` and `BucketAccessRequest` names and namespace.
+ The adapter gets the corresponding `Bucket` and `BucketAccess` instances, and verifies that the BA has been bound.
+ The adapter creates the host files for the secret and endpoint info.
+ At this point the adapter responds to the `NodePublishVolue` request and the kubelet continues to launch the pod

##### Sharing Dynamically Created Buckets (green-brown)
Once a `Bucket` is created, it is discoverable by other users in the cluster (who have been granted the ability to list `Bucket`s or via non-automated methods).  In order to access the `Bucket`, a user must create a `BucketRequest` (BR) that specifies the `Bucket` instance by name. This `BucketRequest` should not specify a `BucketClass` since the `Bucket` instance already exists.
The user also needs to creates a `BucketAccessRequest` (BAR), which references the BR. At this point the workflow is the same as above.

### DeleteBucket

![DeleteBucket Workflow](COSI%20Architecture_Delete%20Bucket%20Workflow.png)

_Delete_ covers deleting a bucket and/or revoking access to a bucket. A `Bucket` delete is triggerd by the user deleting their `BucketRequest`. A `BucketAccess` removal is triggered by the user deleting their `BucketAccessRequest`. A bucket is not deleted if there are any bindings (accessors). Once all bindings have been removed the `Bucket`'s Phase is set to "Released", **and** if the release policy is "Delete", then the sidecar will gRPC call the provisioner's _Delete_ interface. It's up to each provisioner whether or not to physically delete bucket content, but the expectation is that the physical bukcet will at least be made unavailable.

Also, when the app pod terminates, the kubelet will gRPC call `NodeUnpublishVolume` which is received by the cosi-node-adapter. The adapter will ensure that the access granted to this pod is removed, and if this pod is the last accessor, then depending on the bucket's _releasePolicy_, the bucket may be deleted.

> Note: a brownfield `BucketRequest` is not honored if the associated `Bucket`'s _deleteTimestamp_ is set.

> Note: delete is described below as a synchronous workflow but it will likely need to be implemented asynchronously. The steps should still mostly follow what's outlined below.

These are the common steps to delete a `Bucket`. Note: there are atypical workflows where an admin deletes a `Bucket` or a `BucketAccess` instance which are not described here:
+ User deletes their `BucketRequest` (BR) which "hangs" until finalizers have been removed and the BR can finally be deleted.
+ User deletes their `BucketAccessRequest` (BAR) which "hangs" until finalizers have been removed and the BAR can finally be deleted.
+ COSI central controller sees the BR.deleteTimestamp and the BAR.deleteTimestamp are set.
+ COSI deletes the associated `Bucket`, which sets its deleteTimestamp but does not delete it due to finalizer, and its Phase remains "Bound" (presumably).
+ COSI deletes the associated `BucketAccess` (BA), which sets its deleteTimestamp but does not delete it due to finalizer, and its Phase remains "Bound" (presumably).
+ Sidecar sees the deleteTimestamp set in the `BucketAccess` and gRPC calls the provisoner's _Revoke_ interface.
+ COSI unbinds the BA from the `Bucket`.
+ COSI checks for additional binding references and if there are any we stop processing the BR delete (but continue processing the BAR delete).
+ When all the binding references in the `Bucket` have been removed, COSI sets the `Bucket`'s Phase to "Released".
+ Sidecar sees `Bucket.Phase` = "Released" and knows the `Bucket.releasePolicy`.
+ If the release policy is "Delete", the sidecar gRPC calls the provisoner's _Delete_ interface, and upon successful completion, sets the `Bucket`'s Phase to "Deleted"
+ If the release policy is "Retain" then the `Bucket` remains "Released" and it can potentially be reused.
+ When the COSI controller sees the `Bucket`'s Phase is "Deleted", it deletes all the finalizers and the real deletions occur.

---

## Provisioner Secrets

Per [Non-Goals](#non-goals), it is not within the scope of the proposal to abstract IAM operations.  Instead, provisioner and user credentials should be provided to automation by admins or users.  
To allow for flexibility in authorizing provisioner operations, credentials may be provided to the provisioner in several ways.

- **Per Provisioner:** the Secret is used for all provisioning operations, for the lifetime of the provisioner.  These Secrets should be injected directly into the provisioner's container via [common Kubernetes patterns](https://kubernetes.io/docs/tasks/inject-data-application/distribute-credentials-secure/).

Credentials may also be specified at more granular levels in order to allow for context dependent keys.  E.g. When authorization differs between BucketClasses or between individual operations.  This may be facilitated by defining a set of string keys which the core automation will be aware of, so that Secrets may be referenced by BucketClasses.  For example:

```yaml
cosi.io/provisioner-secret-name:
cosi.io/provisioner-secret-namespace:
```

- **Per BucketClass:** A secret may be made specific to a BucketClass.  This suits cases where authorization may be segregated in the object store.  The Secret may then be defined explicitly in the `bucketClass.parameters` map.

  ```yaml
  cosi.io/provisioner-secret-name: "foo"
  cosi.io/provisioner-secret-namespace: "bar"
  ```

- **Per Operation/Bucket:** Unique credentials are passed per Bucket or operation. In order to support dynamic Secret naming, templating similar to [CSI Secret templating](https://kubernetes-csi.github.io/docs/secrets-and-credentials-storage-class.html) may be used.  E.g.

  ```yaml
  "${bucket.name}"
  "${bucket.namespace}"
  ```
  
  Admins may then define a BucketClass with the following parameters included:

  *Per Bucket Operation*
  
  ```yaml
  cosi.io/provisioner-secret-name: "${bucket.name}"
  cosi.io/provisioner-secret-namespace: "${bucket.namespace}"
  ```
---

## gRPC Definitions

There is one service defined by COSI - `Provisioner`. This will need to be satisfied by the vendor-provisioner in order to be COSI-compatible

```
service Provisioner {
    rpc ProvisionerGetInfo (ProvisionerGetInfoRequest) returns (ProvisionerGetInfoResponse) {}

    rpc ProvisionerCreateBucket (ProvisionerCreateBucketRequest) returns (ProvisionerCreateBucketResponse) {}
    rpc ProvisionerDeleteBucket (ProvisionerDeleteBucketRequest) returns (ProvisionerDeleteBucketResponse) {}

    rpc ProvisionerGrantBucketAccess (ProvisionerGrantBucketAccessRequest) returns (ProvisionerGrantBucketAccessResponse);
    rpc ProvisionerRevokeBucketAccess (ProvisionerRevokeBucketAccessRequest) returns (ProvisionerRevokeBucketAccessResponse);
}
```

#### ProvisionerGetInfo

This call is meant to retrieve the unique provisioner Identity. This identity will have to be set in `BucketRequest.Provisioner` field in order to invoke this specific provisioner.

```
message ProvisionerGetInfoRequest {
    // Intentionally left blank
}

message ProvisionerGetInfoResponse {    
    string provisioner_identity = 1;
}
```

#### ProvisonerCreateBucket

This call is made to create the bucket in the backend. If the bucket already exists, then the appropriate error code `ALREADY_EXISTS` must be returned by the provisioner.

```
message ProvisionerCreateBucketRequest {    
    string bucket_name = 1;

    string region = 2;

    string zone = 3;

    map<string,string> bucket_context = 4;

    enum AnonymousBucketAccessMode {
	BUCKET_PRIVATE = 0;
	BUCKET_READ_ONLY = 1;
	BUCKET_WRITE_ONLY = 2;
	BUCKET_READ_WRITE = 3;
    }
    
    AnonymousBucketAccessMode anonymous_bucket_access_mode = 5;

    map<string,string> secrets = 6;
}

message ProvisionerCreateBucketResponse {
    // Intentionally left blank
}
```

#### ProvisonerDeleteBucket

This call is made to delete the bucket in the backend. If the bucket has already been deleted, then no error should be returned.

```
message ProvisionerDeleteBucketRequest {
    string bucket_name = 1;
    
    string region = 2;

    string zone = 3;

    map<string,string> bucket_context = 4;    
}

message ProvisionerDeleteBucketResponse {
     // Intentionally left blank
}
```

#### ProvisionerGrantBucketAccess

This call grants access to a particular principal. Note that the principal is the account for which this access should be granted. 

If the principal is set, then it should be used as the username of the created credentials or in someway should be deterministically used to generate a new credetial for this request. This principal will be used as the unique identifier for deleting this access by calling ProvisionerRevokeBucketAccess

If the `principal` is empty, then a new service account should be created in the backend that satisfies the requested `access_policy`. The username/principal for this service account should be set in the `principal` field of `ProvisionerGrantBucketAccessResponse`.

```
message ProvisionerGrantBucketAccessRequest {
    string bucket_name = 1;
    
    string region = 2;

    string zone = 3;

    map<string,string> bucket_context = 4;  

    string principal = 5;
    
    string access_policy = 6;
}

message ProvisionerGrantBucketAccessResponse {
    // This is the account that is being provided access. This will
    // be required later to revoke access. 
    string principal = 1;
    
    string credentials_file_contents = 2;
    
    string credentials_file_path = 3;
} 
```

#### ProvisionerRevokeBucketAccess

This call revokes all access to a particular bucket from a principal.  

```
message ProvisionerRevokeBucketAccessRequest {
    string bucket_name = 1;
    
    string region = 2;

    string zone = 3;

    map<string,string> bucket_context = 4;  

    string principal = 5;
}

message ProvisionerRevokeBucketAccessResponse {
    // Intentionally left blank
}
```
