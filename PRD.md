# Product Requirements Document (PRD): Budge-it (OpenShift Native Edition)

## 1. Product Overview

**Objective:** To build a web application that allows users to upload their bank and credit card statements in various formats (CSV, PDF, JPEG, PNG). The app will automatically extract transaction data, categorize spending, provide visual analytics, and offer actionable recommendations. The entire stack will be designed, deployed, and managed using Red Hat OpenShift native operators and Custom Resource Definitions (CRDs).

**Target Audience:** Individuals seeking to take control of their personal finances, budget better, and identify wasteful spending without manually entering transactions.

## 2. User Stories

* **As a user,** I want to upload multiple file types (CSV, PDF, image) so that I don't have to manually transcribe my physical or digital statements.
* **As a user,** I want the app to automatically extract the date, merchant, and amount from my uploads so I can see an accurate ledger of my spending.
* **As a user,** I want my transactions automatically grouped into categories (e.g., Groceries, Utilities, Entertainment) so I know where my money goes.
* **As a user,** I want to view charts and graphs of my monthly spending so I can quickly digest my financial habits.
* **As a platform administrator,** I want the application and its dependencies deployed via OpenShift native operators and YAML manifests to ensure automated lifecycle management, high availability, and easy scaling.

## 3. Functional Requirements

### 3.1. Data Ingestion & File Upload

* **Supported Formats:** CSV, PDF, JPEG, PNG.
* **Upload Mechanism:** Drag-and-drop interface with progress indicators.
* **Validation:** System verification of file types and size limits (e.g., 10MB per file).

### 3.2. Processing & OCR Extraction

* **CSV Parsing:** Direct column mapping (Date, Description, Amount, Debit/Credit) to the database schema.
* **Document/Image Parsing:** Utilize an OCR service to extract text from unstructured formats.
* **Data Cleaning:** Automatic sanitization (e.g., stripping out currency symbols, handling asterisks).

### 3.3. Categorization Engine

* **Rule-Based Tagging:** Match merchant names to predefined categories.
* **Fuzzy Matching:** Handle merchant abbreviations (e.g., "AMZN MKTPLACE").
* **Customization:** Allow manual re-categorization that saves as a persistent user rule.

### 3.4. Analytics & Visualization Dashboard

* **Total Outgoings vs. Inflows:** High-level summary of money in vs. money out.
* **Category Breakdown:** Visual charts showing spending percentages by category.

## 4. Technical Architecture (OpenShift Native)

### 4.1. Frontend (React)

* **Framework:** React (using Vite for fast builds).
* **Containerization:** Packaged as an OCI-compliant container image running on Red Hat Enterprise Linux (RHEL) Universal Base Image (UBI).

### 4.2. Backend (Go)

* **Framework:** Go (using Gin or Fiber) for high-performance, concurrent API request handling.
* **Microservices:** Backend API structured to handle asynchronous processing of heavy OCR tasks.
* **Containerization:** Compiled statically and packaged in a RHEL UBI minimal image.

### 4.3. Data Storage & Management (OpenShift Operators)

* **Relational Database:** **Crunchy Data PostgreSQL Operator** (or Red Hat build). Instead of a standard unmanaged database, this operator will automatically handle provisioning, high availability, backups, and disaster recovery for the PostgreSQL cluster storing users, transactions, and category rules.
* **Object Storage (for uploads):** **OpenShift Data Foundation (ODF)**. Uploaded PDFs and images will be stored temporarily using the Multi-Cloud Object Gateway (NooBaa) via an `ObjectBucketClaim` (OBC), providing an S3-compatible API that is fully native to the OpenShift cluster.

### 4.4. CI/CD & Delivery

* **CI/CD Pipeline:** **OpenShift Pipelines (Tekton)** for building the Go and React images via Source-to-Image (S2I) or standard Dockerfiles.
* **GitOps:** **OpenShift GitOps (Argo CD Operator)** to manage and synchronize the OpenShift YAML manifests directly from a Git repository to the cluster.

## 5. OpenShift Deployment & YAML Specifications

To deploy the app and its supporting services, a suite of declarative OpenShift YAML manifests must be created. The engineering team will deliver a Helm chart or Kustomize overlay containing the following OpenShift/Kubernetes resources:

* **Custom Resource Definitions (CRDs) for Operators:**
* `PostgresCluster` YAML: To instruct the Crunchy Data operator to spin up a highly available PostgreSQL cluster.
* `ObjectBucketClaim` (OBC) YAML: To instruct OpenShift Data Foundation to provision an S3-compatible bucket and automatically generate the necessary Secrets and ConfigMaps containing the endpoint and credentials for the Go backend.


* **Compute & Workload Manifests:**
* `Deployment` YAMLs: For both the Go Backend and React Frontend.
* `Service` YAMLs: To expose the frontend and backend pods internally.
* `ServiceAccount` YAMLs: Configured with precise RBAC permissions.


* **Networking Manifests:**
* `Route` YAMLs: OpenShift-specific Edge-terminated Routes to expose the React frontend and Go backend APIs to the public internet securely with auto-generated TLS certificates.


* **Configuration & Security Manifests:**
* `ConfigMap` and `Secret` YAMLs: To securely inject environment variables (e.g., OCR API keys, front-end public URLs).
* `NetworkPolicy` YAMLs: To ensure the React frontend can only talk to the Go backend, and the Go backend can only talk to the PostgreSQL cluster and the ODF bucket.



## 6. Non-Functional Requirements

* **Security & Privacy:** The application must run under restricted OpenShift Security Context Constraints (SCCs). Pods must not run as root. Uploaded files in the ODF bucket must be purged immediately after data extraction.
* **Performance & Scaling:** The Go backend deployment should utilize a `HorizontalPodAutoscaler` (HPA) to scale dynamically based on CPU/Memory usage during batch OCR processing.
* **Observability:** The application must be instrumented to work with the **OpenShift Cluster Observability Operator**, exposing Prometheus metrics for Go API request times and PostgreSQL transaction health.
