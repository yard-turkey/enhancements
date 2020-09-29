# Bound Service Account Tokens

## Table Of Contents

<!-- toc -->

- [Summary](#summary)
- [Background](#background)
- [Motivation](#motivation)
- [Design Details](#design-details)
  - [Token attenuations](#token-attenuations)
    - [Audience binding](#audience-binding)
    - [Time binding](#time-binding)
    - [Object binding](#object-binding)
  - [API Changes](#api-changes)
    - [Add <code>tokenrequests.authentication.k8s.io</code>](#add-)
    - [Modify <code>tokenreviews.authentication.k8s.io</code>](#modify-)
    - [Example Flow](#example-flow)
  - [Service Account Authenticator Modification](#service-account-authenticator-modification)
  - [ACLs for TokenRequest](#acls-for-tokenrequest)
  - [ServiceAccount Admission Controller Migration](#serviceaccount-admission-controller-migration)
    - [Prerequisites](#prerequisites)
    - [Safe rollout of time-bound token](#safe-rollout-of-time-bound-token)
  - [Graduation Criteria](#graduation-criteria)
    - [Alpha-&gt;Beta](#alpha-beta)
    - [Beta -&gt; GA Graduation](#beta---ga-graduation)
- [Production Readiness Review Questionnaire](#production-readiness-review-questionnaire)
  - [Feature Enablement and Rollback](#feature-enablement-and-rollback)
  - [Scalability](#scalability)
  <!-- /toc -->

## Summary

This KEP describes an API that would allow workloads running on Kubernetes to
request JSON Web Tokens that are audience, time and eventually key bound.

## Background

Kubernetes already provisions JWTs to workloads. This functionality is on by
default and thus widely deployed. The current workload JWT system has serious
issues:

1.  Security: JWTs are not audience bound. Any recipient of a JWT can masquerade
    as the presenter to anyone else.
1.  Security: The current model of storing the service account token in a Secret
    and delivering it to nodes results in a broad attack surface for the
    Kubernetes control plane when powerful components are run - giving a service
    account a permission means that any component that can see that service
    account's secrets is at least as powerful as the component.
1.  Security: JWTs are not time bound. A JWT compromised via 1 or 2, is valid
    for as long as the service account exists. This may be mitigated with
    service account signing key rotation but is not supported by client-go and
    not automated by the control plane and thus is not widely deployed.
1.  Scalability: JWTs require a Kubernetes secret per service account.

## Motivation

We would like to introduce a new mechanism for provisioning Kubernetes service
account tokens that is compatible with our current security and scalability
requirements.

## Design Details

Infrastructure to support on demand token requests will be implemented in the
core apiserver. Once this API exists, a client of the apiserver will request an
attenuated token for its own use. The API will enforce required attenuations,
e.g. audience and time binding.

### Token attenuations

#### Audience binding

Tokens issued from this API will be audience bound. Audience of requested tokens
will be bound by the `aud` claim. The `aud` claim is an array of strings
(usually URLs) that correspond to the intended audience of the token. A
recipient of a token is responsible for verifying that it identifies as one of
the values in the audience claim, and should otherwise reject the token. The
TokenReview API will support this validation.

#### Time binding

Tokens issued from this API will be time bound. Time validity of these tokens
will be claimed in the following fields:

- `exp`: expiration time
- `nbf`: not before
- `iat`: issued at

A recipient of a token should verify that the token is valid at the time that
the token is presented, and should otherwise reject the token. The TokenReview
API will support this validation.

Cluster administrators will be able to configure the maximum validity duration
for expiring tokens. During the migration off of the old service account tokens,
clients of this API may request tokens that are valid for many years. These
tokens will be drop in replacements for the current service account tokens.

#### Object binding

Tokens issued from this API may be bound to a Kubernetes object in the same
namespace as the service account. The name, group, version, kind and uid of the
object will be embedded as claims in the issued token. A token bound to an
object will only be valid for as long as that object exists.

Only a subset of object kinds will support object binding. Initially the only
kinds that will be supported are:

- v1/Pod
- v1/Secret

The TokenRequest API will validate this binding.

### API Changes

#### Add `tokenrequests.authentication.k8s.io`

We will add an imperative API (a la TokenReview) to the `authentication.k8s.io`
API group:

```golang
type TokenRequest struct {
  Spec   TokenRequestSpec
  Status TokenRequestStatus
}

type TokenRequestSpec struct {
  // Audiences are the intendend audiences of the token. A token issued
  // for multiple audiences may be used to authenticate against any of
  // the audiences listed. This implies a high degree of trust between
  // the target audiences.
  Audiences []string

  // ValidityDuration is the requested duration of validity of the request. The
  // token issuer may return a token with a different validity duration so a
  // client needs to check the 'expiration' field in a response.
  ValidityDuration metav1.Duration

  // BoundObjectRef is a reference to an object that the token will be bound to.
  // The token will only be valid for as long as the bound object exists.
  BoundObjectRef *BoundObjectReference
}

type BoundObjectReference struct {
  // Kind of the referent. Valid kinds are 'Pod' and 'Secret'.
  Kind string
  // API version of the referent.
  APIVersion string

  // Name of the referent.
  Name string
  // UID of the referent.
  UID types.UID
}

type TokenRequestStatus struct {
  // Token is the token data
  Token string

  // Expiration is the time of expiration of the returned token. Empty means the
  // token does not expire.
  Expiration metav1.Time
}

```

This API will be exposed as a subresource under a serviceaccount object. A
requestor for a token for a specific service account will `POST` a
`TokenRequest` to the `/token` subresource of that serviceaccount object.

#### Modify `tokenreviews.authentication.k8s.io`

The TokenReview API will be extended to support passing an additional audience
field which the service account authenticator will validate.

```golang
type TokenReviewSpec struct {
  // Token is the opaque bearer token.
  Token string
  // Audiences is the identifier that the client identifies as.
  Audiences []string
}
```

#### Example Flow

```
> POST /apis/v1/namespaces/default/serviceaccounts/default/token
> {
>   "kind": "TokenRequest",
>   "apiVersion": "authentication.k8s.io/v1",
>   "spec": {
>     "audience": [
>       "https://kubernetes.default.svc"
>     ],
>     "validityDuration": "99999h",
>     "boundObjectRef": {
>       "kind": "Pod",
>       "apiVersion": "v1",
>       "name": "pod-foo-346acf"
>     }
>   }
> }
{
  "kind": "TokenRequest",
  "apiVersion": "authentication.k8s.io/v1",
  "spec": {
    "audience": [
      "https://kubernetes.default.svc"
    ],
    "validityDuration": "99999h",
    "boundObjectRef": {
      "kind": "Pod",
      "apiVersion": "v1",
      "name": "pod-foo-346acf"
    }
  },
  "status": {
    "token":
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJz[payload omitted].EkN-[signature omitted]",
    "expiration": "Jan 24 16:36:00 PST 3018"
  }
}
```

The token payload will be:

```
{
  "iss": "https://example.com/some/path",
  "sub": "system:serviceaccount:default:default,
  "aud": [
    "https://kubernetes.default.svc"
  ],
  "exp": 24412841114,
  "iat": 1516841043,
  "nbf": 1516841043,
  "kubernetes.io": {
    "serviceAccountUID": "c0c98eab-0168-11e8-92e5-42010af00002",
    "boundObjectRef": {
      "kind": "Pod",
      "apiVersion": "v1",
      "uid": "a4bb8aa4-0168-11e8-92e5-42010af00002",
      "name": "pod-foo-346acf"
    }
  }
}
```

### Service Account Authenticator Modification

The service account token authenticator will be extended to support validation
of time and audience binding claims.

### ACLs for TokenRequest

The NodeAuthorizer will allow the kubelet to use its credentials to request a
service account token on behalf of pods running on that node. The
NodeRestriction admission controller will require that these tokens are pod
bound.

### ServiceAccount Admission Controller Migration

#### Prerequisites

Before migration to a version with `BoundServiceAccountVolume=true`, cluster
operators should make sure:

1.  Set feature gate `TokenRequest=true`. (default to `true` since 1.12)

    - This feature requires the following flags to the API server:
      - `--service-account-issuer`
      - `--service-account-signing-key-file`
      - `--service-account-key-file`
      - `--api-audiences` (default to `--service-account-issuer`)

2.  Set feature gate `TokenRequestProjection=true`. (default to `true` since
    1.12)

3.  Update all workloads to newer version of officially supported Kubernetes
    client libraries to reload token:

    - Go: >= v0.15.7
    - Python: >= v12.0.0
    - Java: >= v9.0.0
    - Javascript: >= v0.10.3
    - Ruby: master branch
    - Haskell: v0.3.0.0

    For community-maintained client libraries, feel free to contribute to them
    if the reloading logic is missing.

    **Note**: If having trouble in finding places using in-cluster config
    completely, cluster operators can specify flag
    `--service-account-extend-token-expiration` to kube apiserver to allow
    tokens have longer expiration temporarily during the migration. Any usage of
    legacy token will be recorded in both metrics and audit logs. After fixing
    all the potentially broken workloads, don't forget to remove the flag so
    that the original expiration settings are honored.

    - Metrics: `serviceaccount_stale_tokens_total`
    - Audit: looking for `authentication.k8s.io/stale-token` annotation

    See next section for the details of how to discover the workloads that will
    suffer from expired tokens.

If anything goes wrong, please file a bug and CC @kubernetes/sig-auth-bugs. More
contact information
[here](https://github.com/kubernetes/community/tree/master/sig-auth#contact).

#### Safe rollout of time-bound token

Legacy service account tokens distributed via secrets are not time-bound. Many
client libraries have come to depend on this behavior. After time-bound service
account token being used, if in-cluster clients do not periodically reload token
from projected volume, requests would be rejected once the initial token got
expired.

In order to allow guadual adoption of time-bound token, we would:

1.  Pick a constant period D between one and two hours. The value of D would be
    static across Kubernetes deployments, while avoiding collision with common
    duration.
1.  Modify service account admission control to inject token valid for D when
    the BoundServiceAccountTokenVolume feature is enabled.
1.  Modify kube apiserver TokenRequest API. When it receives TokenRequest with
    requested valid period D, extend the token lifetime to one year. At the same
    time, save the original requested D to `kubernetes.io/warnafter` field in
    minted token.
1.  In the TokenRequest status, tell clients that the token would be valid only
    for D, encouraging clients to reload token as if the token was valid for D.

This modification could be optionally enabled by providing a command line flag
to kube apiserver.

These extended tokens would not expire and continue to be accepted within one
year. At the same time, the authentication side could monitor whether clients
are properly reloading tokens by:

1.  Compare the `kubernetes.io/warnafter` field with current time. If current
    time is after `kubernetes.io/warnafter` field, it implies calling client is
    not reloading token regularly.
1.  Expose metrics to monitor number of legacy and stale token used.
1.  Add annotation to audit events for legacy and stale tokens including
    necessary information to locate problematic client.

### Graduation Criteria

#### Alpha->Beta

Estimated version: v1.20

All known migration frictions have been fixed:

- PodSecurityPolicies that allow secrets but not projected volumes will
  prevent the use of token volumes.
  - Fixed in https://github.com/kubernetes/kubernetes/pull/92006
- In-cluster clients that don’t reload service account tokens will start
  failing an hour after deployment.
  - Mitigation added in https://github.com/kubernetes/kubernetes/issues/68164
- Pods running as non root may not access the service account token.
  - Fixed in https://github.com/kubernetes/kubernetes/pull/89193

An upgrade test is passing periodically:

1. Create pod A with feature disabled where pod A is working and a secret volume
   is mounted.
2. Enable feature where pod A continue working
3. Create pod B and it is working and projected volumes are mounted.

#### Beta -> GA Graduation

Estimated version: v1.21+

New `ServiceAccount` admission controller WAI in Beta for >= 1 minor without
significant issues.

## Production Readiness Review Questionnaire

### Feature Enablement and Rollback

- **How can this feature be enabled / disabled in a live cluster?**

  - Feature gate name: `BoundServiceAccountTokenVolume`
  - Components depending on the feature gate: kube-apiserver and
    kube-controller-manager
  - Will enabling / disabling the feature require downtime of the control
    plane? yes, need to restart kube-apiserver and kube-controller-manager.
  - Will enabling / disabling the feature require downtime or reprovisioning
    of a node? no.

- **Does enabling the feature change any default behavior?** yes, pods'
  service account tokens will not be long-lived and are not stored as Secrets
  any more.

- **Can the feature be disabled once it has been enabled (i.e. can we roll
  back the enablement)?** yes. pods created while the feature was enabled will
  reference a configmap that can grow stale with the feature disabled.

- **What happens if we reenable the feature if it was previously rolled
  back?** the same as the first enablement.

- **Are there any tests for feature enablement/disablement?**
  - unit test: plugin/pkg/admission/serviceaccount/admission_test.go
  - upgrade test: test/e2e/upgrades/serviceaccount_admission_controller_migration.go

### Scalability

- **Will enabling / using this feature result in any new API calls?**

  - API call type: `TokenRequest`
  - estimated throughput: 1/pod every ~48 minutes.
  - originating component: kubelet
  - components listing and/or watching resources they didn't before: N/A.
  - API calls that may be triggered by changes of some Kubernetes resources:
    N/A.
  - periodic API calls to reconcile state (e.g. periodic fetching state,
    heartbeats, leader election, etc.): 1 call per pod every ~48 minutes.

- **Will enabling / using this feature result in introducing new API types?**
  no.

- **Will enabling / using this feature result in any new calls to the cloud
  provider?** no.

- **Will enabling / using this feature result in increasing size or count of
  the existing API objects?** no.

- **Will enabling / using this feature result in increasing time taken by any
  operations covered by [existing SLIs/SLOs]?** no.

- **Will enabling / using this feature result in non-negligible increase of
  resource usage (CPU, RAM, disk, IO, ...) in any components?** it adds a
  token minting operation in the API server every ~48 minutes for every pod.
