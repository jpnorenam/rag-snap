# A worked example

This is an abridged manifest for an imagined telco-cloud compliance matrix, in the JSON
shape you must return. It is shortened for brevity — a real manifest for a document like
this carries every one of its requirement rows, often one to two hundred — but the shape,
the level of detail in `prompt`, and the relationship between question ids and `domains`
patterns are exactly what is expected of you.

Note in particular:

- Question ids are the matrix's own row ids (`A1`, `C2`, `J1.1`), never renumbered.
- `source` records the sheet each row came from.
- Every `domains` pattern matches ids that are actually present. `J1.*` and `J2.*` are
  split because they are different sub-sections, and each carries more literal characters
  than a bare `J*` would, so each wins for its own rows.
- `E4` gets its own single-question entry, because that one row needs context the rest of
  its section does not.
- The `prompt` is long and specific. That is the point of it.

```json
{
  "version": "1.0",
  "knowledge_bases": ["example-kb-past-answers", "example-kb-platform-docs", "example-kb-support-terms"],
  "kapa_source_groups": ["example-source-group-id"],
  "prompt": "You are a solutions engineer completing the \"Vendor Response\" column of a telco-cloud compliance matrix. Each question you receive is one requirement row lifted out of that spreadsheet. Your answer is pasted verbatim into that row's single cell and read by an evaluator scoring compliance.\n\nPROPOSED SOLUTION — the only thing you ever describe\nThe proposed platform is the vendor's OpenStack distribution for virtual machines and its Kubernetes distribution for containers, both on the vendor's Linux with a commercial support subscription. The customer's incumbent stack is an older release of the same OpenStack distribution, so every answer is a like-for-like-or-better replacement claim: show the capability in the proposed solution, not merely in the incumbent one. Never describe a competing platform as the proposal.\n\nTHE INPUT IS A LABEL, NOT A FULL QUESTION\nQuestions arrive as short requirement labels, often two or three words (\"SR-IOV\", \"Create VM\", \"Cinder Driver Support\"). The spreadsheet's surrounding columns — description, current environment, evaluator guidance — are NOT given to you. Treat the label as the requirement itself and answer the implicit question: does the proposed solution provide this, and by what mechanism. Never ask for clarification, never comment on the terseness, never answer with a definition of the term. The evaluator wants a vendor commitment, not a glossary entry.\n\nANSWER SHAPE — one spreadsheet cell\nTwo to four sentences of flowing prose, at most 100 words. Declarative third person. No bullet points, no numbered lists, no headers, no markdown, no line breaks used as structure — the answer goes into a single cell and any of those will corrupt it. No preamble, no restating the label, nothing like \"Based on the context\". Lead with the answer, substantiate, stop.\n\nFor a capability or support row, open with a plain declaration — \"Supported.\" or \"Not supported.\" — then substantiate. For a lifecycle row, state that the operation is available and name the interface that exposes it: API, CLI, or dashboard.\n\nBE SPECIFIC OR BE SILENT\nSpecifics are what score. Where the context supplies them, name the project or component that delivers the capability, the configuration surface (flavor extra specs, host aggregates, scheduler filters, storage classes, volume access modes), the release, and the support commitment. Never invent a version, date, throughput or latency figure, customer name, reference deployment, headcount, or certification. An unsupported specific is worse than a general true statement.\n\nTHIRD-PARTY HARDWARE AND INTEGRATIONS\nMany labels name a product the customer already owns. Answer at the interface, not the brand: the volume driver and multipath, the CSI driver and its access modes, SR-IOV and DPDK on standard PCI NICs, identity federation over OIDC or SAML, log forwarding to an external SIEM collector. State that any product implementing that documented interface is supported on that basis. Only name a specific third-party product if the retrieved context explicitly documents it; a product named in the label is not itself evidence. Never present an unverified third-party product as certified, validated, or endorsed.\n\nCOMMERCIAL ROWS\nLicensing, pricing, subscription-inclusion and support-tier rows are answered from the context only. State no figure, rate, discount, or contractual term that is not in the context.",
  "domains": [
    {
      "match": "A*",
      "context": "Sheet A, general platform requirements: which OpenStack and Kubernetes releases are supported, the hypervisor and container runtime, host OS compatibility, and backward compatibility with the incumbent release. Answer at the level of supported releases and the support lifecycle attached to them.",
      "keywords": ["release cadence", "LTS", "supported until", "KVM", "containerd", "upgrade path", "backward compatibility"]
    },
    {
      "match": "C*",
      "context": "Sheet C, Enhanced Platform Awareness: the NFV performance features a telco data plane depends on. Every row here should name the configuration surface that enables the feature and the scheduler behaviour that places the workload, not merely assert support.",
      "keywords": ["CPU pinning", "NUMA", "huge pages", "SR-IOV", "DPDK", "PCI passthrough", "flavor extra specs", "host aggregate", "affinity", "anti-affinity"]
    },
    {
      "match": "E*",
      "context": "Sheet E, virtual machine lifecycle operations. Each row is one operation. State that the operation is available and name the interface that exposes it — API, CLI, or dashboard — rather than describing the platform generally.",
      "keywords": ["Nova", "server action", "resize", "live migration", "evacuate", "instance lifecycle"]
    },
    {
      "match": "E4",
      "context": "Sheet E, the auto-heal row specifically. This is the one lifecycle row that is not a single API call: describe the instance-evacuation and health-check mechanism, what detects the failure, and what the operator must configure for recovery to be automatic. Do not claim automatic recovery without naming the component that performs it.",
      "keywords": ["instance HA", "evacuation", "health check", "fencing", "compute node failure", "automatic recovery"]
    },
    {
      "match": "J1.*",
      "context": "Sheet J, sub-section 1: compute platform capabilities in the abstract — core counts, memory, generation support. Answer on the platform's supported limits and the mechanism that enforces them.",
      "keywords": ["compute", "cores per socket", "memory overcommit", "CPU generation", "supported limits"]
    },
    {
      "match": "J2.*",
      "context": "Sheet J, sub-section 2: a named server manufacturer the customer has already bought. State the support position first, then the interface it rests on — certified hardware programmes, standard PCI devices, out-of-band management. Do not present the manufacturer as certified unless the retrieved context says so.",
      "keywords": ["hardware certification", "server vendor", "BMC", "Redfish", "IPMI", "commissioning"]
    },
    {
      "match": "GIS*",
      "context": "Security baseline implementation activities and their required deliverables. Reserved for rows that may appear in a later revision of this matrix.",
      "keywords": ["hardening", "CIS benchmark", "STIG", "security baseline", "audit"]
    }
  ],
  "questions": [
    { "id": "A1", "question": "Supported OpenStack releases and upgrade path", "source": "A. General" },
    { "id": "A2", "question": "Container runtime support", "source": "A. General" },
    { "id": "C1", "question": "SR-IOV", "source": "C. Enhanced Platform Awareness" },
    { "id": "C2", "question": "CPU Pinning", "source": "C. Enhanced Platform Awareness" },
    { "id": "C3", "question": "Huge Pages", "source": "C. Enhanced Platform Awareness" },
    { "id": "E1", "question": "Create VM", "source": "E. VM Lifecycle" },
    { "id": "E2", "question": "Delete VM", "source": "E. VM Lifecycle" },
    { "id": "E3", "question": "Manual scale out", "source": "E. VM Lifecycle" },
    { "id": "E4", "question": "Auto heal", "source": "E. VM Lifecycle" },
    { "id": "J1.1", "question": "Maximum cores per compute node", "source": "J. Hardware" },
    { "id": "J1.2", "question": "Memory overcommit support", "source": "J. Hardware" },
    { "id": "J2.1", "question": "Support for the incumbent server manufacturer", "source": "J. Hardware" }
  ]
}
```

## On the reserved pattern

`GIS*` above matches none of the listed questions. That is deliberate and legitimate — the
matrix has a security-baseline section whose rows are not in this revision, and the entry is
kept so it takes effect when they appear. rag-cli reports a zero-match pattern as a warning
rather than an error for exactly this reason.

Do not imitate this by default. Write patterns against ids you actually emitted; reserve one
only when the document itself tells you the section exists.

## A second example: a document of shorthand notes

The example above is a compliance matrix whose rows are named capabilities, so the terse
labels are left alone. Many documents are not like that. This one is a requirements
worksheet whose cells hold several questions each, written as notes — and it is the case
that goes wrong most often, because leaving a cell whole produces one crammed answer that
addresses its first clause and neglects the rest.

The source worksheet has fifteen rows. It contains far more than fifteen questions.

```
Row title: "Delivery Model"
Cell:      "_ Full platform handover / Partial handover with Vendor support ?
            _ Hardware included / Bring-Your-Own hardware ?
            _ Vendor Support model and supported platform components ?"

Row title: "Sovereignty & Legal"
Cell:      "_interoperability, open and standardized API, export data and workloads
            _control over encryption keys.
            _transparent data flows & access logs."
```

That becomes **six** entries, not two. Each is expanded into a question that stands on its
own, because the answering model sees it with no document and no row title in view. The
ids are short section codes with a per-question suffix, and the full row title goes in
`source`:

```json
{
  "questions": [
    { "id": "R02-a", "question": "Can the platform be delivered as a full handover to the provider's own operations team, or as a partial handover in which the vendor retains part of the operational responsibility?", "source": "Delivery Model" },
    { "id": "R02-b", "question": "Is hardware included as part of the platform delivery, or is the platform deployed on hardware the provider brings itself?", "source": "Delivery Model" },
    { "id": "R02-c", "question": "What is the vendor's support model for the platform, and which platform components are covered by that support?", "source": "Delivery Model" },
    { "id": "R03-a", "question": "Which open and standardised APIs does the platform expose, and by what mechanisms can data and workloads be exported off the platform to another environment?", "source": "Sovereignty & Legal" },
    { "id": "R03-b", "question": "Does the operator retain control over encryption keys, and what key management is available for data at rest and in transit?", "source": "Sovereignty & Legal" },
    { "id": "R03-c", "question": "What data flows out of the platform to the vendor or to any third party, and what access logging and audit records are available to the operator?", "source": "Sovereignty & Legal" }
  ],
  "domains": [
    { "match": "R02-*", "context": "Delivery and operating model: how the platform is handed over, who operates it, whether hardware is in scope, and what the support commitment covers.", "keywords": ["handover", "managed service", "support tier", "bring your own hardware"] },
    { "match": "R03-*", "context": "Sovereignty and legal: open APIs and data portability, key custody, data flows and audit logging, and the documentation and licensing that let the operator run the platform autonomously.", "keywords": ["open API", "data export", "encryption keys", "audit log", "open source licence", "air-gapped"] }
  ]
}
```

What to take from this:

- **Six entries from two rows.** The row count is not the question count.
- **Every question stands alone.** "_control over encryption keys." would have arrived at
  the answering model as four words with no subject; expanded, it asks something answerable.
- **Nothing was invented.** Each expansion says only what the shorthand already meant. The
  fourth bullet of a row does not become two questions to pad the count.
- **`R02-*` and `R03-*` route.** Had the row titles been used as ids, every question in a
  row would share one id, they would collide, and a glob over "Sovereignty & Legal" would
  be a poor pattern.
- **A row that is only a label gets no entry.** A row reading "References" is not a question.
