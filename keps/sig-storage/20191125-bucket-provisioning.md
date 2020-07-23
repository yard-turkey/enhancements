---
# Title

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
  - [gRPC](#grpc)
    - [Create](#create)
    - [Delete](#delete)
  - [Workflows](#workflows)
    - [Create]
      - [Greenfield](#greenfield)
      - [Brownfield](#brownfield)
    - [Delete]
      - [Greenfield](#greenfield)
      - [Brownfield](#brownfield)
  - [Provisioner Secrets](#provisioner-secrets)
<!-- /toc -->
# Summary

This proposal introduces the *Container Object Storage Interface* (COSI), a system composed of Custom Resource Definitions (CRDs), a controller architecture, and a gRPC specification, for the purpose of standardizing object storage representations in Kubernetes.  Goals and non-goals set the scope for the proposal by defining higher level objectives.  The vocabulary section defines terminology.  User stories illustrate how these APIs may fulfill cluster user requirements.  Relationships between the APIs are provided to illustrate the interconnections between object storage APIs, users' workloads, and object store service instances.  Lastly, the documents states the proposed API specs for the BucketRequest, Bucket, BucketClass, and various access related objects.

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
+ Bucket access management is not within the scope of this KEP.  ACLs, access policies, and credentialing need to be handled out of band.

##  Vocabulary

+ _adapter_ - a pod per node which receives Kubelet gRPC nodePublish and nodeUnpublish requests, ensures the target bucket has been provisioned, and notifies the kubelet that the pod can be run.
+ _brownfield bucket_ - an existing storage instance which could be part of a Kubernetes cluster or completely separate.
+ _BucketRequest_ - a user-namespaced custom resource representing a request for a storage instance endpoint.
+ _BucketClass_ - a cluster-scoped custom resource containing fields defining the provisioner and an immutable parameter set for creating new buckets
+ _Bucket_ - a cluster-scoped custom resource referenced by a `BucketRequest` and containing connection information and metadata for a storage instance.
+ _greenfield bucket_ - a new bucket created by automation.
+ _object_ - an atomic, immutable unit of data stored in buckets.
+ _storage instance_ - refers to the back object storage endpoint being abstracted by the Bucket API (a.k.a “bucket” or “container”).
+ _driverless_ - a system where no driver is deployed to automate object store operations.  COSI automation may still be deployed to managed COSI APIs.  See [Architecture](#architecture).

# Proposal

## User Stories

#### Admin

- As a cluster administrator, I can control access to new and existing buckets when accessed from the cluster, regardless of the backing object store.

#### User

- As a developer, I can define my object storage needs in the same manifest as my workload, so that deployments are streamlined and encapsulated within the Kubernetes interface.
- As a developer, I can define a manifest containing my workload and object storage configuration once, so that my app may be ported between clusters as long as the storage provided supports my designated data path protocol.
- As a developer, I want to create a workload controller which is bucket API aware, so that it can dynamically connect workloads to object storage instances.

## APIs

> NOTE: For a graphical mapping of how these APIs relate to each other, see [API Relationships](#api-relationships).



### Storage APIs

#### BucketRequest

A namespaced API representing a workload’s need for a storage instance endpoint. Created in the workload's namespace.  A [BucketAccessRequest](#bucketaccessrequest) is required in order to gain credentialed access to the storage instance.


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
  secretName: [6]
  bucketName: [7]
status:
  phase: [8]
  conditions: 
```

1. `labels`: added by the controller.  Key’s value should be the provisioner name. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
1. `finalizers`: added by controller to defer BucketRequest deletion until backend deletion ops succeed.
1. `protocol`: (required) specifies the desired protocol.  One of {“s3”, “gcs”, or “azureBlob”}.
1. `bucketPrefix`: (optional) prefix prepended to a randomly generated bucket name, eg. “yosemite-photos-". If empty no prefix is prepended. If `bucketName` is also supplied then it overrides `prefix'.
1. `bucketClassName`: (optional) name of the target BucketClass used for greenfield provisioning only. If omitted, a default bucket class matching the protocol is searched for. If the BucketClass does not support the requested protocol, an error is logged and retries occur.
1. `secretName`: (optional) Secret in the BucketRequest's namespace storing credentials to be used by a workload for bucket access.
1. `bucketName`: (optional) name of the actual backend bucket. If blank then COSI will fill in the name of the created storage bucket. If filled in by the user, then that name is used as the complete bucket name in the object store (no randomness added by COSI).
1. `phase`: 
   - *Pending*: The controller has detected the new `BucketRequest` and begun provisioning operations.
   - *Bound*: Provisioning operations have completed and the `BucketRequest` has been bound to a `Bucket`.
   - *Deleting*: The controller has detected deletion of the `BucketRequest` and begun the delete operation.

> Note: additionally there are some error phases, such as *ErrBucketClassDoesNotSupportProtocol*, *ErrBucketDeletionInProgress*, etc.

#### Bucket

A cluster-scoped resource representing a storage instance.  A `Bucket` contains information to enable the requesting client to access the storage instance.  At a minimum, a Bucket stores enough identifying information so that drivers can accurately target the storage instance (e.g. during a deletion process).  There is a 1-to-many relationship between a `Bucket` and a `BucketRequest`, meaning that many `BucketRequest`s can reference the same `Bucket`.

For greenfield, COSI creates the `Bucket` based on values in the `BucketRequest` and `BucketClass`. For brownfield, an admin manually creates the `Bucket` and COSI takes care of binding and populating fields returned by the provisioner.

```yaml
apiVersion: cosi.io/v1alpha1
kind: Bucket
Metadata:
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
  permittedNamespaces: [8]
    - name:
      uid:
  protocol: [9]
    protocolSignature: ""
    azureBlob: [10]
      containerName:
      storageAccount:
    s3: [11]
      endpoint:
      bucketName:
      region:
      signatureVersion:
    gcs: [12]
      bucketName:
      privateKeyName:
      projectId:
      serviceAccount:
  parameters: [13]
status:
  message: [14]
  phase: [15]
  conditions:
```

1. `name`: For greenfield, generated in the pattern of _"bucket-"<bucketRequest.name>"-"<bucketRequest.namespace>_. 
1. `labels`: added by the controller.  Key’s value should be the provisioner name. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
1. `finalizers`: added by the controller to defer `Bucket` deletion until backend deletion ops succeed.
1. `provisioner`: The provisioner field defined in the `BucketClass`.  Used by sidecars to filter Buckets.
1. `releasePolicy`: Prescribes outcome of a Delete events. **Note:** In brownfield, the `Bucket`'s release policy is always set to "Retain", and thus backing storage instance is never deleted.
   - _Delete_:  the bucket and its contents are destroyed.
   - _Retain_:  the bucket and its data are preserved with only abstracting Kubernetes being destroyed.
1. `anonymousAccessMode`:  ACL specifying *uncredentialed* access to the Bucket.  This is applicable to cases where the storage instance or objects are intended to be publicly readable and/or writable.  Accepted values:
   - `private`: Default, disallow uncredentialed access to the storage instance.
   - `ro`: Read only, uncredentialed users can call ListBucket and GetObject.
   - `rw`: Read/Write, same as `ro` with the addition of PutObject being allowed.
   - `wo`: Write only, uncredentialed users can only call PutObject.
1. `bucketClassName`: Name of the associated BucketClass (greenfield only).
1. `permittedNamespaces`: An array of namespaces, identified by a name and uid, from which  BucketRequests are allowed to bind to the Bucket.  Provided to allow admins a layer of cluster-layer access control.  Does **not** reflect or alter the backing storage instances' ACLs or IAM policies.
   - In greenfield, the originating BuckerRequest’s namespace must be specified at time of Bucket generation.
   - In brownfield, this list is defined by the admin.
1. `protocol`: The protocol the application will use to access the storage instance.
   - `protocolSignature`: Specifies the protocol targeted by this Bucket instance.  One of:
     - `azureBlob`: data required to target a provisioned azure container and/or storage account.
     - `s3`: data required to target a provisioned S3 bucket and/or user.
     - `gcs`: data required to target a provisioned GCS bucket and/or service account.
1. `parameters`: a copy of the BucketClass parameters.
1. `message`: a human readable description detailing the reason for the current `phase``.
1. `phase`: is the current state of the Bucket:
   - _Creating: the controller is in the process of provisioning the bucket, meaning creating a new bucket or granting access to an existing bucket.
   - _Deleting: the Bucket is unbound and ready to be deleted.
   - _Deleted: the physical bucket has been deleted and the `Bucket` is about to be removed.
   - _Bound: access to a bucket has been granted, and, for greenfield, a new bucket was created. The `Bucket` is bound to a `BucketRequest`.
   - _Released_: the `Bucket` is unbound and available for reuse.

> Note: a "Released" 'Bucket's can be reused by brownfield `BucketRequest`s.

#### BucketClass

A cluster-scoped API whose purpose it to provide admins control over the handling of new (greenfield) bucket provisioning.  The `BucketClass` defines a release policy, specifies driver specific parameters, and provides the provisioner name. A default BucketClass can be defined for each supported protocol, which allows the bucket class to be omitted from a `BucketRequest`.

> Note: Bucket classes are not used for brownfield access provisioning. Instead, an admin manually creates a `Bucket` and the user references this `Bucket` in their `BucketRequest`.

```yaml
apiVersion: cosi.io/v1alpha1
kind: BucketClass
metadata:
  name: 
provisioner: [1]
isDefaultBucketClass: [2]
supportedProtocols: {"azureblob", "gcs", "s3", ... } [3]
anonymousAccessMode: {"ro", "wo", "rw"} [4]
additionalPermittedNamespaces: [5]
- name:
  uid: 
releasePolicy: {"Delete", "Retain"} [6]
parameters: [7]
```

1. `provisioner`: (required) the name of the driver. If supplied the driver container and sidecar container are expected to be deployed. If omitted the `secretRef` is required for static provisioning.
1. `isDefaultBucketClass`: (optional) boolean, default is false. If set to true then potentially a `BucketRequest` does not need to specify a `BucketClass`. If the greenfield `BucketRequest` omits the `BucketClass` and a default `BucketClass`'s supported protocol matches the `BucketRequest`'s protocol then the default bucket class is used. **Note:** there can only be one supported protocol for default bucket classes.
1. `supportedProtocols`: (required) protocol(s) supported by the associated object store. This field serves two purposes: 1) recognition that some object store support more than one protocol, 2) validation that the `BucketRequest`'s desired protocol is supported.  **Note:** default bucket classes can define only one supported protocol.
1. `anonymousAccessMode`: (optional) ACL specifying *uncredentialed* access to the Bucket.  This is applicable for cases where the storage instance or objects are intended to be publicly readable and/or writable.
1. `additionalPermittedNamespaces`: (optional) a list of namespaces *in addition to the originating namespace* that will be allowed access to this Bucket.
1. `releasePolicy`: (required) defines bucket retention for greenfield `BucketRequest` deletes only. **Note:** In brownfield and static cases, *Retain* is mandated. 
   - `Delete`:  the bucket and its contents are destroyed.
   - `Retain`:  the bucket and its data are preserved with only abstracting Kubernetes being destroyed.
1. `parameters`: (optional) a map of string:string key values.  Allows admins to control user and access provisioning by setting provisioner key-values.  **Note:** see [Provisioner Secrets](#provisioner-secrets) for some predefined `parameters` settings.

### Access APIs

The Access APIs abstract the backend policy system.  Access policy and user identities are an integral part of most object stores.  As such, a system must be implemented to manage both user/credential creation and the binding of those users to individual buckets via policies.  Object stores differ from file and block storage in how they manage users, with cloud providers typically integrating with an IAM platform.  This API includes support for cloud platform identity integration with Kubernetes ServiceAccounts.  On-prem solutions usually provide their own user management systems, which may look very different from each other and from IAM platforms.  We must also account for third party authentication solutions that may be integrated with an on-prem service.

#### BucketAccessRequest

A user facing API representing an object store user and an access policy defining the user’s relation to a storage instance.  A user creates a `BucketAccessRequest` in the app's namespace (which is the same namespace as the `BucketRequest`).  A BucketAccessRequest can specify *either* a ServiceAccount or a desired Secret name.  Specifying a ServiceAccount enables provisioners to support cloud provider identity integration with their respective Kubernetes cluster offerings.

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
  bucket: [5] 
  bucketAccessClassName: [6]
  bucketAccessName: [7]
status:
  message: [8]
  phase: [9]
```

1. `labels`: added by the controller.  Key’s value should be the provisioner name. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
1. `finalizers`: added by the controller to defer `BucketAccessRequest` deletion until backend deletion ops succeed.
1. `serviceAccountName`: (optional) the name of a Kubernetes ServiceAccount in the same namespace.  This field is included to support cloud provider identity integration.  Should not be set when specifying `accessSecretName`.
1. `accessSecretName`: (optional) the name of a Kubernetes Secret in the same namespace.  This field is used when there is not cloud provider identity integration.  Should not be set when specifying `serviceAccountName`.
1. `bucket`: The the name of the `Bucket` instance to which the user identity or ServiceAccount should be granted access to, according to the policies defined in the `BucketAccessClass`.
1. `bucketAccessClassName`: name of the `BucketAccessClass` specifying the desired set of policy actions to be set for a user identity or ServiceAccount.
1. `bucketAccessName`: name of the bound cluster-scoped `BucketAccess` instance.
1. `message`: a human readable description detailing the reason for the current `phase``.
1. `phase`: is the current state of the Bucket:
   - *Pending*: The controller has detected the new `BucketAccessRequest` and begun provisioning operations.
   - *Bound*: Provisioning operations have completed and the `BucketAccessRequest` has been bound to a `BucketAccess`.
   - *Deleting*: The controller has detected deletion of the `BucketAccessRequest` and begun the delete operation.  **Note:** additionally there may be some error phases.

#### BucketAccess

A cluster-scoped administrative API which encapsulates fields from the BucketAccessRequest and the BucketAccessClass.  The purpose of the API is to serve as communication path between provisioners and the central COSI controller.  In greenfield, the COSI controller creates `BucketAccess` instances for new `BucketAccessRequest`s.  There is one `BucketAccess` instance per `BucketAccessRequest`.

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
  bucketAccessRequestNamespace: [4]
  serviceAccountName: [5]
  accessSecretName: [6]
  provisioner: [7]
  parameters: [8]
 status:
  message: [9]
  phase: [10]
```

1. `name`: For greenfield, generated in the pattern of `"bucketAccess-"<bucketAccessRequest.name>"-"<bucketAccessRequest.namespace>`. 
1. `labels`: added by the controller.  Key’s value should be the provisioner name. Characters that do not adhere to [Kubernetes label conventions](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) will be converted to ‘-’.
1. `finalizers`: added by the controller to defer `BucketAccess` deletion until backend deletion ops succeed.
1. `bucketAccessRequestName`/`bucketAccessRequestNamespace`: name and namespace of the bound `BucketAccessRequest`
1. `serviceAccountName`: name of the Kubernetes ServiceAccount specified by the `BucketAccessRequest`.  Undefined when the `BucketAccessRequest.accessSecretName` is defined.
1. `  accessSecretName`: name of the *provisioner* generated `Secret` containing access credentials. This `Secret` exists in the provisioner’s namespace and must be copied to the app namespace by the COSI controller.
1. `provisioner`:  name of the provisioner that should handle this `BucketAccess` instance.  Copied from the `BucketAccessClass`.
1. `parameters`:  A map of string, string key values.  Allows admins to control user and access provisioning by setting provisioner key-values.
1. `message`: a human readable description detailing the reason for the current `phase``.
1. `phase`: is the current state of the Bucket:
   - _Bound_: the controller finished processing the request and has bound the BucketAccess to the BucketAccessRequest
   - _Released_: the originating Bucket has been deleted, signalling that the Bucket is ready for garbage collection.  This will occur on greenfield Buckets once all requests referencing the Bucket are deleted.
   - _Failed_: error and all retries have been exhausted.
   - _Retrying_: set when a driver or Kubernetes error is encountered during provisioning operations indicating a retry loop.

#### BucketAccessClass

A cluster-scoped API providing a way for admins to specify policies that may be used to access buckets.  These are always applicable in greenfield, where access is dynamically granted, and only sometimes applicable in brownfield, where a user identity may already exist, but not yet have access to a bucket.  In this case, a BucketAccessRequest will still specify the BucketAccessClass in order to determine which actions it is granted to a defined bucket.

```yaml
apiVersion: cosi.io/v1alpha1
kind: BucketAccessClass
metadata: 
  name:
provisioner: [1]
supportedProtocols: [2]
policyActions: [3]
  allow:
  - "*"
  deny:
  - "*"
parameters: [4]
```

1. `provisioner`: The name of the provisioner that `BucketAccess` instances should be managed by.
1. `supportedProtocols`: protocols the associated object store supports.  Applied when matching Bucket to BucketClasses.  Admins may specify more than protocol when necessary.  `BucketAccessRequests.spec.protocol` will be checked against this array prior to provisioning.
1. `policyActions`: a set of provisioner/platform defined policy actions to allow or deny a given user identity.
1. `parameters`:   (Optional)  A map of string, string key values.  Allows admins to control user and access provisioning by setting provisioner key-values.



---

## COSI Architecture (Warning - Construction Area)

### Topology
(Diagram to be added)

## Workflows
Here we describe the workflows used to create/provision new or existing buckets and to delete/de-provision buckets.

>  Note: Per [Non-Goals](#non-goals), access management is not within the scope of this KEP.  ACLs, access policies, and credentialing should be handled out of band.  Buckets may be configured to allow access for specific namespaces.

### Create
Create covers creating a new bucket and/or granting access to an existing bucket. In both cases the custom resources (CRs) described above are instantiated.

"Greenfield" defines a new bucket created based on a user's [BucketRequest](#bucketrequest), and access granted to this bucket.
“Brownfield” describes any case where the bucket already exists. This bucket is abstracted by a [Bucket](#Bucket) which has been instantiated. There can be multiple `BucketRequests` that bind to a single `Bucket`.

> Note: COSI determines that a request is for a new bucket when an associated `Bucket` instance does not exist.  Consider that `BucketRequest.bucketClassName` can be blank because COSI supports default bucket classes. Also, `BucketRequest.bucketName` can be filled in because the user wants a specific bucket name for their new bucket.

Prep for brownfield:
+ admin creates a `Bucket` with most of the fields filled in.
+ note that a `BucketClass` is not used for brownfield.

Here is the workflow:
+ COSI central controller detects a new `BucketRequest` (BR).
+ COSI central controller detects a new `BucketAccessRequest`(BAR).
+ COSI gets the `BR.BucketClass` (directly or via the matching default).
+ COSI gets the `BAR.bucketAccessClass` object.
+ COSI creates the associated `Bucket`, after filling in a template consisting of fields from the `BucketClass` and BR.
  + If this create operation fails due to the `Bucket` already existing, then we have a brownfield case; otherwise it's greenfield.  **Note:** the goal here is to reduce separate greenfield vs. brownfield logic in the code.
+ COSI creates the associated `BucketAccess` (BA), after filling in a template consisting of fields from the `BucketAccessClass` and BAR.
+ For a newly created `Bucket`, the sidecar sees it and gRPC calls the driver to create a bucket.
  + COSI creates the `Bucket` instance.
+ COSI is done with the `BucketRequest`, but there is no binding until the `BucketAccess` instance is created for the associated `BucketAccessRequest`.
+ The sidecar sees the newly created BA and gRPC calls the driver to grant access.
+ COSI sees the completed BA which triggers binding the BA to the BR. This is accomplished via lables in the `Bucket`.

##### Sharing Dynamically Created Buckets (green-brown)
Once a `Bucket` is created, it is discoverable by other users in the cluster (who have been granted the ability to list `Bucket`s or via non-automated methods).  
In order to access the `Bucket`, a user must create a `BucketRequest` (BR) that specifies the `Bucket` by name. This `BucketRequest` should not specify a `BucketClass` since the `Bucket` instance already exists.
The user also needs to creates a `BucketAccessRequest` (BAR), which references the BR. At this point the workflow is the same as above.

### Delete
Delete covers deleting a newly created bucket and revoking access to a bucket. In all cases the bucket contents is _not_ deleted **except** for greenfield buckets with a `releasePolicy` of "Delete". In this one case, the sidecar will gRPC call the provisioner's _Delete_ interface and it's up to each provisioner whether or not to physically delete bucket content.

> Note: a `Bucket` cannot be deleted as long as there is one access to that `Bucket`, meaning at least one binding of the `BucketAccess` instance to the `BucketRequest`. So deleting a `Bucket` implies also deleting access to that `Bucket`. The converse is not true -- a `BucketAccessRequest` can be deleted without deleting the `Bucket`.

> Note: delete is described below as a synchronous workflow but it will likely need to be implemented asynchronously. The steps should still mostly follow what's outlined below.

These are the common steps to delete a `Bucket`. Note: there are atypical workflows where an admin deletes a `Bucket` or a `BucketAccess` instance which are not described here:
+ User deletes their `BucketRequest` (BR).
+ User deletes their `BucketAccessRequest` (BAR).
+ COSI central controller sees the BR.deleteTimestamp set (BR not deleted due to finalizer).
+ COSI central controller sees the BAR.deleteTimestamp set (BAR not deleted due to finalizer).
+ COSI deletes the associated `Bucket`, which sets its deleteTimestamp but does not delete it due to finalizer.
+ COSI deletes the associated `BucketAccess` (BA), which sets its deleteTimestamp but does not delete it due to finalizer.
+ Sidecar sees the deleteTimestamp set in the BA and gRPC calls the provisoner's _Revoke_ interface.
+ COSI removes the `Bucket` label for this BA.
+ COSI looks at the `Bucket`'s labels for binding references. If there are any we stop at this point. The `Bucket`'s deleteTimestamp is set and its Phase is still "Bound", but the driver will not be invoked.
+ When all the binding labels are gone, COSI sets the `Bucket`'s Phase to "Deleting".
+ The sidecar sees `Bucket.Phase` = "Deleting" and knows the `Bucket.releasePolicy`.
+ If the release policy is "Delete" the sidecar gRPC calls the provisoner's _Delete_ interface. **Note:** it is crucial that COSI always sets the `Bucket.releasePolicy` to "Retain" except when the `BucketClass` defines the policy as "Delete". There is potentially a corner case where a brownfield `BucketRequest` references a BucketClass whose releasePolicy is "Delete". COSI needs to ignore all BucketClass settings in this situation.
+ If the release policy is "Retain" then its Phase is set to "Released" and potentially it can be reused.
+ When the sidecar sets the `Bucket`'s Phase to "Deleted", then COSI deletes all the finalizers and the real deletions occur.

Outside of COSI, the admin creates the `Bucket` and communicates its name to the user.
The user creates a `BucketRequest` referencing this `Bucket` by name. There is no `BucketClass` required.
COSI detects the new `BucketRequest`, sees that a matching `Bucket` exists, invokes the provisioner to grant this user access to the bucket, and binds the `BucketRequest` to the `Bucket`.


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

# GRPC Definitions
```protobuf
 syntax = "proto3";

 package cosi.v1;

 import "google/protobuf/descriptor.proto";

 option go_package = "github.com/container-object-store-interface/go-cosi";

 extend google.protobuf.MessageOptions {

     // cosi_secret should be used to designate messages containing sensitive data
     //             to provide protection against that data being logged or otherwise leaked.
     bool cosi_secret = 1000;
 }

 message DriverInfoRequest {
     // INTENTIONALLY BLANK
 }

 // DataProtocol defines a set of constants used in Create and Grant requests.
 enum DataProtocol {
     PROTOCOL_UNSPECIFIED = 0;
     AZURE_BLOB = 1;
     GCS = 2;
     S3 = 3;
 }

 // AccessMode defines a common set of permissions among object stores
 enum AccessMode {
     MODE_UNSPECIFIED = 0;
     RO = 1;
     WO = 2;
     RW = 3;
 }

 // S3SignatureVersion defines the 2 supported versions of S3's authentication
 enum S3SignatureVersion {
     VERSION_UNSPECIFIED = 0;
     V2 = 1;
     V4 = 2;
 }

 message DriverInfoResponse {
     // DriverName
     string DriverName = 1;

     // SupportedProtocols
     repeated DataProtocol SupportedProtocols = 2;

     // NextId = 3;
 }
 
 message S3Context {
     // returns the location where bucket will be created
     string location = 1
 }

message GCSContext {
   // returns the location where bucket is created
   string location = 1
   // returns the project the bucket belongs to
   string project = 2
}

message AzureContext {
}

message GenericContext {
     // generic output content
     map<string, string> bucket_data
}

message ProviderContext {
     oneof {
          S3Context
          GCSContext
          AzureContext
          GenericContext
     }
}

message Bucket {
  // Name is the name of the bucket
  string name = 1
  // provisioner used to create and other bucket operations
  // ProjectName this bucket created under
  ProviderContext provider_context = 2 //- { azure_context, gcs_context, s3_context, generic_context}
  string provisioner = 3
  // access mode
  AccessMode access_mode = 4;
    
}


 message CreateBucketRequest {
     // bucket_name, This field is REQUIRED.
     // Maintain Idempotency. 
     //    In the case of error, the CO MUST handle the gRPC error codes
     //    per the recovery behavior defined in the "CreateBucket Errors"
     //    section below.
     // BucketRequest:name 
     string bucket_name = 1;

     // RequestProtocol, one of the predefined values
     // Driver must check the protocol used to match 
     // BucketClass:supportedProtocols - {"azureblob", "gcs", "s3", ... } [3]
     // BucketRequest:protocol - use this as request protocol but check 
     // if the protocol is in the BucketClass' suppportedProtocols
     DataProtocol request_protocol = 2;

     // DriverParameters, these are parameters that are extracted from 
     // BuckerRequest and BucketClass so that the call has context.
     // For example GCS require projectName for CreateBucket to succeed.
     // BucketClass:provisioner - identify the  
     // projectID if GCS
     ProviderContext provider_context = 3 //- { azure_context, gcs_context, s3_context, generic_context}
     map<string, string> driver_parameters = 4;  

     // AccessMode is requested as RO, RW, WO and depends on driver.
     // If driver supports access mode if not ignores it
     // BucketClass:accessMode - {"ro", "wo", "rw"} [4]
     AccessMode access_mode = 5;

     // Information required to make createBucket call. This field is REQUIRED
     // A series of tokens, user name, etc based on protocol choice
     // BucketRequest:secretName will provide necessary security token to
     // connect to the provider API.  
     // Azure:
     //    message AuthenticationData {
     //        option (cosi_secret) = true;
     //        string StorageAccountName = 1;
     //        string AccountKey = 2;
     //        string SasToken = 3;
     //    }
     // GCS:
     //    message AuthenticationData {
     //        option (cosi_secret) = true;
     //        string StorageAccountName = 1;
     //        string PrivateKeyName = 2;
     //        string PrivateKey = 3;
     //    }
     // S3:
     //    message AuthenticationData {
     //        option (cosi_secret) = true;
     //        string AccessKeyId = 1;
     //        string SecretKey = 2;
     //        string StsToken = 3;
     //        string UserName = 4;
     //    }
     map<string, string> secrets = 6;
 }


CREATE_INVALID_ARGUMENT    : validation of the input argument fails 
CREATE_INVALID_PROTOCOL    : driver does not support the protocol
CREATE_ALREADY_EXISTS      : resource already exists 
CREATE_INVALID_CREDENTIALS : resource creation failed due to invalid credentials 
CREATE_INTERNAL_ERROR      : Failed to execute the requested call

 message CreateBucketResponse {
     // Bucket returned
     Bucket bucket
 }
 
 message DeleteBucketRequest {
     // The name of the bucket to be deleted.
     // This field is REQUIRED.
     string bucket_name = 1;
    
     // RequestProtocol, one of the predefined values
     // Driver must check the protocol used to match 
     // BucketClass:supportedProtocols - {"azureblob", "gcs", "s3", ... } [3]
     // BucketRequest:protocol - use this as request protocol but check 
     // if the protocol is in the BucketClass' suppportedProtocols
     DataProtocol request_protocol = 2;

     // DriverParameters, these are parameters that are extracted from 
     // BuckerRequest and BucketClass so that the call has context.
     // For example GCS require projectName for CreateBucket to succeed.
     // BucketClass:provisioner - identify the  
     // projectID if GCS
     ProviderContext provider_context = 3 - { azure_context, gcs_context, s3_context, generic_context}
     map<string, string> driver_parameters = 4; //provider_context
  
     // Secrets required by driver to complete bucket deletion request.
     // This field is OPTIONAL. Refer to the `Secrets Requirements`
     // section on how to use this field.
     // Azure:
     //    message AuthenticationData {
     //        option (cosi_secret) = true;
     //        string StorageAccountName = 1;
     //        string AccountKey = 2;
     //        string SasToken = 3;
     //    }
     // GCS:
     //    message AuthenticationData {
     //        option (cosi_secret) = true;
     //        string StorageAccountName = 1;
     //        string PrivateKeyName = 2;
     //        string PrivateKey = 3;
     //    }
     // S3:
     //    message AuthenticationData {
     //        option (cosi_secret) = true;
     //        string AccessKeyId = 1;
     //        string SecretKey = 2;
     //        string StsToken = 3;
     //        string UserName = 4;
     //    }
     map<string, string> secrets = 5
}



 message DeleteBucketResponse {
     // INTENTIONALLY BLANK
 }

DELETE_BUCKET_DOESNOT_EXIST : Bucket specified does not exist 
DELETE_DELETE_INPROGRESS    : Delete bucket is in progress
DELETE_INVALID_CREDENTIALS  : resource deletion failed due to invalid credentials
DELETE_INTERNAL_ERROR       : Failed to execute the requested call
DELETE_INVALID_ARGUMENT     : validation of the input argument fails 



service CosiController {
  rpc CreateBucket (CreateBucketRequest)
    returns (CreateBucketResponse) {}

  rpc DeleteBucket (DeleteBucketRequest)
    returns (DeleteBucketResponse) {}
}


 message S3Credentials {
      string id = 1 // one of id, emailid, uri
      string permission = 1
      string owner
 }

message GCSCredentials {
     string entity = 1 //one ot userid, emailid, groupid, etc or 'allusers/allAuthenticatedUsers
     string role = 2
     string domain = 3
     string project = 4
}

message AzureCredentials {
     string id
     string permission
}

message GenericCredentials {
     // generic output content
     map<string, string> credentials
}

message ProviderCredentials {
     oneof {
          S3Credentials
          GCSCredentials
          AzureCredentials
          GenericCredentials
     }
}


 message GrantBucketAccessRequest {
     // The name of the bucket to be granted access.
     // This field is REQUIRED.
     string bucket_name = 1;
    
     // RequestProtocol, one of the predefined values
     // Driver must check the protocol used to match 
     // BucketClass:supportedProtocols - {"azureblob", "gcs", "s3", ... } [3]
     // BucketRequest:protocol - use this as request protocol but check 
     // if the protocol is in the BucketClass' suppportedProtocols
     DataProtocol request_protocol = 2;

     // permission granted
     map<string,string> permissions = 3 
     
     // provisioner used to create and other bucket operations
     // ProjectName this bucket created under
     ProviderContext provider_context = 4 - { azure_context, gcs_context, s3_context, generic_context}
     
     map<string,string> secrets
 }

 message GrantBucketAccessResponse {
     // No data returned by this call other than error or success code
     repeated ProviderCredentials creds
 }
 
 
GRANT_BUCKET_DOESNOT_EXIST : Bucket specified does not exist 
GRANT_INVALID_CREDENTIALS  : resource deletion failed due to invalid credentials
GRANT_INTERNAL_ERROR       : Failed to execute the requested call
GRANT_INVALID_ARGUMENT     : validation of the input argument fails
GRANT_INVALID_PRINCIPAL    : Failed to grant, principal provided is invalid 
 
 

 message RevokeBucketAccessRequest {
     // The name of the bucket to be granted access.
     // This field is REQUIRED.
     string bucket_name = 1;
    
     // RequestProtocol, one of the predefined values
     // Driver must check the protocol used to match 
     // BucketClass:supportedProtocols - {"azureblob", "gcs", "s3", ... } [3]
     // BucketRequest:protocol - use this as request protocol but check 
     // if the protocol is in the BucketClass' suppportedProtocols
     DataProtocol request_protocol = 2;

     // the service_account from which permissions are revoked
     ProviderCredentials service_account

     // permission revoked
     map<string,string> permissions = 3 
     
     // provisioner used to create and other bucket operations
     // ProjectName this bucket created under
     ProviderContext provider_context = 4 - { azure_context, gcs_context, s3_context, generic_context}
     
     map<string,string> secrets
     
 }

 message RevokeBucketAccessResponse {
     // No data returned by this call other than error or success code
     repeated ProviderCredentials creds    
 }

REVOKE_BUCKET_DOESNOT_EXIST : Bucket specified does not exist 
REVOKE_INVALID_CREDENTIALS  : resource deletion failed due to invalid credentials
REVOKE_INTERNAL_ERROR       : Failed to execute the requested call
REVOKE_INVALID_ARGUMENT     : validation of the input argument fails
REVOKE_INVALID_PRINCIPAL    : Failed to grant, principal provided is invalid 



 service CosiController {
     rpc GrantBucketAccess (GrantBucketAccessRequest) returns (GrantBucketAccessResponse);
     rpc RevokeBucketAccess (RevokeBucketAccessRequest) returns (RevokeBucketAccessResponse);
}


