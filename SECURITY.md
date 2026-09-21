# Security Policy

The TypeSafe Go community takes security seriously and follows responsible disclosure.

## Reporting a vulnerability

Email [security@stacklok.com](mailto:security@stacklok.com) with affected versions, reproduction steps, and relevant files. Do not open a public issue. If the repository's private GitHub Security Advisory form is available, reporters may alternatively [use it](https://github.com/stacklok/typesafe-go/security/advisories/new). For GPG-encrypted disclosure, first email to request the current public key.

## Disclosure process

Suspected vulnerabilities are handled under the [Responsible Disclosure model](https://en.wikipedia.org/wiki/Responsible_disclosure). Immediately email the security team about an already-public vulnerability. The team will ask reporters to keep reports private until a fix is available.

## Patch, release, and communication

Timelines are targets for private disclosures and may change with severity, development effort, upstream coordination, or public disclosure (which is handled as soon as possible).

### Fix team organization (within 24 hours)

The security team selects relevant engineers, adds them to the private advisory, and forms a Fix Team. Details, severity, and handling decisions remain in the private advisory.

### Fix development (within 1–7 days)

The team maintains the repository advisory, records affected versions and CVSS assessment, requests a CVE when appropriate, develops and reviews the fix in the advisory's private fork, and agrees on a release date. Communication stays in private channels. A low assessed risk may justify a slower schedule.

### Fix disclosure (within 1–21 days)

The Fix Team and security team release the reviewed fix, publish the advisory and actionable mitigations, and announce the release, CVE, severity, and impact on the [Stacklok Discord](https://discord.gg/stacklok). Release timing and artifacts depend on the affected package.

## Retrospective

Within 1–3 days after release, the team conducts a blameless retrospective covering contributors, timeline, cause, response, and improvements, and shares an appropriate summary with the Stacklok community.
