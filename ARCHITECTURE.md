# Budge-it — Architecture

This document describes how Budge-it is put together: the runtime topology on
OpenShift, the statement-processing pipeline, the data model, the network
security contract, and the CI/CD flow. The product requirements are in
[PRD.md](PRD.md).

## 1. Runtime topology (OpenShift)

Everything below the Routes is declared in `deploy/base/` and applied per
environment through the Kustomize overlays in `deploy/overlays/`.

```mermaid
flowchart TB
    user([User browser])

    subgraph cluster["OpenShift cluster"]
        subgraph ingress["openshift-ingress"]
            router[Router<br/>edge TLS termination]
        end

        subgraph ns["namespace: budge-it"]
            feRoute[/"Route: budgeit"/]
            beRoute[/"Route: budgeit-api"/]

            subgraph feDeploy["Deployment: budgeit-frontend (×2)"]
                fe[nginx · UBI<br/>serves SPA, proxies /api]
            end

            subgraph beDeploy["Deployment: budgeit-backend (HPA 2–10)"]
                be[Go API + worker pool]
            end

            hpa[HorizontalPodAutoscaler<br/>CPU 70% · memory 80%]

            subgraph pgc["PostgresCluster: budgeit-db (Crunchy operator)"]
                pg1[(postgres<br/>primary)]
                pg2[(postgres<br/>replica)]
                backrest[pgBackRest<br/>scheduled backups]
            end

            obc[ObjectBucketClaim:<br/>budgeit-uploads]
        end

        subgraph storage["openshift-storage (ODF)"]
            noobaa[(NooBaa MCG<br/>S3 endpoint)]
        end

        subgraph mon["Monitoring"]
            prom[Prometheus<br/>user-workload]
        end
    end

    ocr[External OCR service]

    user -->|HTTPS| router
    router --> feRoute --> fe
    router --> beRoute --> be
    fe -->|/api → :8080| be
    be -->|DATABASE_URL from<br/>pguser secret| pg1
    pg1 <-->|replication| pg2
    pg1 --- backrest
    obc -.->|generates ConfigMap + Secret<br/>BUCKET_*, AWS_*| be
    be -->|S3 API| noobaa
    be -->|HTTPS, PDF/images| ocr
    hpa -.->|scales| beDeploy
    prom -->|scrape /metrics<br/>ServiceMonitor| be
```

Key wiring details:

- The **Crunchy operator** turns the single `PostgresCluster` CR into an HA
  pair with scheduled pgBackRest backups, and generates the secret
  `budgeit-db-pguser-budgeit`; the backend reads its `uri` key as
  `DATABASE_URL`.
- The **ObjectBucketClaim** makes ODF provision an S3 bucket and emit a
  ConfigMap (`BUCKET_HOST/PORT/NAME`) plus Secret (`AWS_ACCESS_KEY_ID`,
  `AWS_SECRET_ACCESS_KEY`), both mounted into the backend via `envFrom` — no
  hand-managed storage credentials.
- The **HPA owns the backend replica count** (the Deployment declares none,
  and the Argo CD Application ignores replica drift on it).
- Both containers run under the **restricted SCC**: non-root, no privilege
  escalation, all capabilities dropped, read-only root filesystem on the
  backend, and no service-account tokens mounted.

## 2. Statement processing pipeline

Uploads return `202 Accepted` immediately; extraction happens on an
in-process worker pool (default 4 workers, `WORKERS` env). Jobs interrupted
by a pod restart are recovered on startup by re-enqueuing every upload still
marked `pending`/`processing`.

```mermaid
sequenceDiagram
    actor U as User
    participant FE as Frontend (nginx/SPA)
    participant BE as Backend API
    participant S3 as ODF bucket
    participant DB as PostgreSQL
    participant W as Worker pool
    participant OCR as OCR service

    U->>FE: drop statement file
    FE->>BE: POST /api/v1/uploads (multipart)
    BE->>BE: validate type (CSV/PDF/JPEG/PNG) + 10 MB limit
    BE->>S3: put staged object
    BE->>DB: INSERT upload (status=pending)
    BE-->>FE: 202 Accepted {id, status}
    BE->>W: enqueue(upload id)

    Note over FE: polls GET /uploads every 2.5 s<br/>while any upload is busy

    W->>DB: status=processing
    W->>S3: get staged object

    alt CSV
        W->>W: parse columns, clean amounts
    else PDF / JPEG / PNG
        W->>OCR: POST file (multipart)
        OCR-->>W: extracted text
        W->>W: line heuristics → date/merchant/amount
    end

    W->>DB: load user category rules
    W->>W: categorize (rules → keywords → fuzzy)
    W->>DB: batch INSERT transactions,<br/>status=done
    W->>S3: DELETE staged object (privacy purge)

    FE->>BE: GET /analytics/summary, /analytics/categories
    BE->>DB: aggregate queries
    BE-->>FE: charts + ledger refresh
```

### Categorization engine

Three tiers, first match wins; descriptions are normalized first
(uppercase, punctuation collapsed: `AMZN*Mktplace-0442` → `AMZN MKTPLACE 0442`).

```mermaid
flowchart LR
    d[Normalized description] --> r{User rule<br/>substring match?}
    r -->|yes| cat[Category]
    r -->|no| k{Built-in keyword<br/>~100 merchants?}
    k -->|yes| cat
    k -->|no| f{Fuzzy token match<br/>Levenshtein ≤ 1–2?}
    f -->|yes| cat
    f -->|no| u[Uncategorized<br/>credits default to Income]

    ui[UI re-categorization] -->|PATCH createRule=true| rules[(category_rules)]
    rules --> r
    ui -.->|retro-tags matching<br/>Uncategorized rows| tx[(transactions)]
```

Re-categorizing in the UI persists a rule keyed on the merchant, so the
correction applies to every future statement — and immediately retro-tags
existing uncategorized transactions for the same merchant.

## 3. Data model

```mermaid
erDiagram
    users ||--o{ uploads : owns
    users ||--o{ transactions : owns
    users ||--o{ category_rules : owns
    uploads ||--o{ transactions : "extracted into"

    users {
        uuid id PK
        text email UK
        timestamptz created_at
    }
    uploads {
        uuid id PK
        uuid user_id FK
        text filename
        text content_type
        bigint size_bytes
        text status "pending | processing | done | error"
        text error
        text object_key "cleared after purge"
        int txn_count
        timestamptz created_at
        timestamptz processed_at
    }
    transactions {
        uuid id PK
        uuid user_id FK
        uuid upload_id FK
        date txn_date
        text description "as printed on statement"
        text merchant "normalized"
        numeric amount "always positive"
        text direction "debit | credit"
        text category
    }
    category_rules {
        uuid id PK
        uuid user_id FK
        text pattern "normalized merchant fragment, unique per user"
        text category
    }
```

Schema lives in `backend/internal/db/migrations/` (embedded in the binary and
applied on startup, tracked in `schema_migrations`). A single default user is
seeded until authentication lands; every query is already scoped by
`user_id`, so adding auth is a matter of swapping the subject in
`api.userID()`.

## 4. Network security contract

`deploy/base/networkpolicies.yaml` enforces the PRD's traffic rules with a
default-deny ingress baseline. Arrows below are the **only** allowed flows.

```mermaid
flowchart LR
    router[openshift-ingress<br/>router] -->|8080| fe[frontend pods]
    router -->|8080| be[backend pods]
    fe -->|8080 only| be
    mon[openshift-monitoring] -->|8080 /metrics| be
    mon -->|9187 exporter| pg
    be -->|5432| pg[postgres pods]
    be -->|443| odf[openshift-storage<br/>NooBaa S3]
    be -->|443, public CIDRs only| ocr[external OCR]
    pg <-->|replication| pg

    deny[/"default-deny-all (ingress)"/]
```

Notes:

- Frontend egress is restricted to DNS + the backend; it cannot reach the
  database or the bucket at all.
- Backend egress to the OCR service allows public HTTPS only (RFC-1918
  ranges excluded); tighten it to a specific `ipBlock` or in-cluster selector
  once the OCR endpoint is fixed.
- ServiceAccounts carry **no RBAC grants** and do not mount API tokens —
  neither workload talks to the Kubernetes API.

## 5. CI/CD and GitOps

Build and deploy are decoupled: Tekton produces images and bumps the prod
overlay; Argo CD is the only thing that touches the cluster.

```mermaid
flowchart LR
    dev([Developer]) -->|git push| repo[(Git repository)]
    repo -->|webhook| el[EventListener<br/>budge-it-ci]
    el --> pr[PipelineRun: budgeit-build]

    subgraph pipeline["Tekton pipeline"]
        clone[git-clone] --> test[go vet + go test]
        test --> bb[buildah:<br/>backend image]
        test --> bf[buildah:<br/>frontend image]
        bb --> bump[kustomize edit set image<br/>deploy/overlays/prod]
        bf --> bump
    end

    pr --> clone
    bb -->|push| reg[(Internal image registry)]
    bf -->|push| reg
    bump -->|commit + push| repo

    argo[Argo CD Application<br/>openshift-gitops] -->|watches<br/>deploy/overlays/prod| repo
    argo -->|sync · prune · self-heal| ns[namespace budge-it]
    reg -.->|image pull| ns
```

- Both images build from **Red Hat UBI** bases (Go toolset → UBI minimal;
  Node.js toolset → UBI nginx) so they run unprivileged under the restricted
  SCC.
- The prod overlay pins image tags; promotion is a Git commit, so every
  rollout is auditable and revertible with `git revert`.
- Dev and prod share the same Kustomize base; dev patches replica counts and
  runs a single-instance database (`deploy/overlays/dev`).

## 6. Observability

- The backend exposes `/metrics` (Prometheus format), scraped through the
  `ServiceMonitor` by user-workload monitoring / the Cluster Observability
  Operator.
- Application metrics: `budgeit_http_request_duration_seconds` (per route,
  method, status), `budgeit_uploads_total`, `budgeit_processing_jobs_total`
  (by outcome), `budgeit_processing_job_duration_seconds`,
  `budgeit_transactions_extracted_total`, `budgeit_processing_queue_depth`,
  plus Go runtime metrics.
- PostgreSQL health comes from the Crunchy `pgmonitor` exporter enabled on
  the `PostgresCluster` CR (port 9187, allowed by NetworkPolicy).
- Probes: `/healthz` (liveness) and `/readyz` (readiness, pings the
  database) drive the Deployment rollout and Route backends.
