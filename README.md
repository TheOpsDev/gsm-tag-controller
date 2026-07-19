# gsm-tag-controller

A Kubernetes controller that binds pre-existing GCP **Resource Manager Tags** to
Google Secret Manager (GSM) secrets provisioned by External Secrets Operator
(ESO) `PushSecret` resources.

## Design

See the [reconciliation flow diagram](gsm-tag-controller.svg) for the full flow.

### Architecture

**Watched resources:**

- **`PushSecret`** (primary trigger) — on provisioning, the controller ensures
  an associated `GSMSecretTag` exists and checks its status
- **`GSMSecretTag`** (owned CRD) — holds desired tags in spec, actual GCP
  tagging state in status

**Resource relationship:** 1:1. `GSMSecretTag` is namespaced, created in the
same namespace as its `PushSecret`, with an `ownerReference` for garbage
collection.

### The `GSMSecretTag` CRD

```yaml
apiVersion: gsm-tag-controller.io/v1alpha1
kind: GSMSecretTag
metadata:
  name: <derived from PushSecret name>
  namespace: <same as PushSecret>
  ownerReferences: [<the PushSecret>]
spec:
  secretTarget: <from PushSecret's RemoteKey>   # GSM secret name
  tags:
    environment: prod
    team: payments
    namespace: ds-back
  deletionPolicy: Retain | Delete   # default: Retain
status:
  conditions: [...]                 # Ready, error messages (e.g. missing tag key)
  observedTags: {...}               # what's actually bound in GCP
```

### Tag source

Tags come from an annotation on the `PushSecret`:

```yaml
annotations:
  gsm-tag-controller.io/tags: "environment=prod,team=payments,namespace=ds-back"
```

Values are **short names**, resolved to `tagKeys/{id}` / `tagValues/{id}` under
the **organization** at reconcile time.

### Reconcile flow

1. `PushSecret` event → check ESO has provisioned the secret
2. Ensure `GSMSecretTag` exists (create with spec derived from annotation +
   RemoteKey if missing)
3. Compare `.status` (actual) vs `.spec` (desired)
4. If drift: **full sync** — add missing bindings, *remove* bindings no longer
   in spec
5. On success: update `.status`, exit gracefully
6. On failure (API error, missing tag key/value): write error to
   `.status.conditions`, **requeue with backoff** — never terminal, since
   GCP-side state can change out-of-band

### Deletion

- `PushSecret` deleted → `GSMSecretTag` garbage-collected via ownerReference
- `deletionPolicy: Retain` (default) → GCP tag bindings left in place
- `deletionPolicy: Delete` → finalizer on `GSMSecretTag` removes GCP bindings
  first

### Configuration (Helm values)

- **GCP org ID** — parent for tag key/value resolution (`organizations/{id}`)
- **GCP project ID** — used to build the secret's full resource name:
  `//secretmanager.googleapis.com/projects/{project}/secrets/{name}`
- **Auth:** Workload Identity (no keys)

## Installation

The Helm chart lives in `dist/chart`. Both GCP values are required:

```sh
helm install gsm-tag-controller dist/chart \
  --namespace gsm-tag-controller-system --create-namespace \
  --set gcp.organizationID=123456789 \
  --set gcp.projectID=my-project
```

For GKE Workload Identity, annotate the chart's ServiceAccount with the GCP
service account that holds tag binding permissions:

```yaml
serviceAccount:
  annotations:
    iam.gke.io/gcp-service-account: gsm-tag-controller@my-project.iam.gserviceaccount.com
```

### Known limitations (accepted)

- Single GCP project per controller deployment (project comes from Helm
  values, not resolved per-SecretStore)
- Tag keys/values must pre-exist in GCP — the controller only binds, never
  creates

## End-to-end verification

The full pipeline has been verified against a kind cluster with ESO
installed (2026-07-19). Without GCP credentials the controller cannot
complete a real tag binding, so a syntactically valid but unsigned service
account key was mounted: the controller executes its real GCP code path and
requests reach the Resource Manager API, which rejects them at
authentication — exercising the designed failure surface.

Setup:

1. `kind create cluster`, install ESO from `charts.external-secrets.io`
2. `docker build` the controller image, `kind load` it, install `dist/chart`
   with `gcp.organizationID`/`gcp.projectID` set
3. Create a `SecretStore` using ESO's `fake` provider, a source `Secret`,
   and a `PushSecret` annotated with `gsm-tag-controller.io/tags`

Verified behaviors:

- **Creation** — once the `PushSecret` went Ready, the controller created a
  `GSMSecretTag` with tags parsed from the annotation (whitespace trimmed),
  `secretTarget` taken from the RemoteKey, `deletionPolicy` defaulted to
  `Retain`, and a controller ownerReference
- **Error surfacing** — status showed `Ready: False / SyncFailed` carrying
  the actual GCP API error (`Unauthenticated`), confirming tag resolution
  runs against the real Resource Manager API
- **Requeue with backoff** — repeated retries logged while the GCP error
  persisted
- **Annotation updates** — editing the annotation propagated to `spec.tags`
  within seconds
- **Garbage collection** — deleting the `PushSecret` removed the
  `GSMSecretTag` via ownerReference; the `Retain` finalizer path released
  without GCP calls
- **`deletionPolicy: Delete`** — deletion was correctly held by the
  finalizer while GCP cleanup could not succeed, and released as soon as the
  policy was patched to `Retain`

Not yet exercised: a successful tag binding, which needs real credentials
(Workload Identity in-cluster, or local ADC with `make run`) plus
pre-existing tag keys/values under a real organization.
