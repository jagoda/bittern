# 0001. Adopt architecture decision records

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0002](./0002-product-scope-and-vision.md), [vision](../vision.md),
  [architecture](../architecture.md), [implementation plan](../plan/implementation-plan.md)

## Context

Bittern is pre-implementation. The initial design session produced a large set of interlocking
choices about the NVR engine, host platform, networking, configuration, updates, security, and
release process. Many of those choices have credible alternatives and non-obvious costs.

The project is also intended to be handed to future contributors and coding agents who will not
have the original conversation. The repository already requires that agents read the documentation
before changing code. Conversation transcripts and commit messages are poor places to recover
rationale: they are long, unordered, and usually omit the alternatives that were rejected.

There is no product code yet, so the cost of establishing a decision process is low. The cost of
retrofitting one after implementation starts is high.

## Decision

Adopt architecture decision records (ADRs) as the project's decision log.

- Every architecturally significant decision gets a numbered ADR in `docs/adr/`.
- ADRs use the MADR-lite template in [0000-template.md](./0000-template.md).
- Files are named `NNNN-sentence-case-title.md`, with four-digit numbers that are never reused.
- ADRs move through `Proposed`, `Accepted`, `Superseded by ADR-NNNN`, and `Deprecated`.
- Accepted ADRs are immutable in substance. A changed decision is written as a new ADR that
  supersedes the old one, with links in both directions.
- The index in [README.md](./README.md) is maintained by hand and is the entry point for readers.
- The bootstrap records 0002 through 0013 capture decisions already taken before implementation.
  Later decisions, including those currently listed in [open questions](../open-questions.md), are
  written as new ADRs when they are resolved.
- Architecture, plan, and component documents link to the ADRs that constrain them.

## Consequences

### Positive

- Rationale, constraints, and rejected alternatives survive contributor and agent turnover.
- New contributors can read the log in number order and understand the shape of the system.
- Writing the alternatives and costs forces sharper decisions and exposes weak assumptions.
- Security, privacy, licensing, and update decisions become auditable rather than implicit.
- Reviewers have a stable reference when a change appears to contradict an earlier choice.

### Negative / costs

- Every significant decision now carries a writing and review cost.
- The log can drift from the code if ADRs are not updated by supersession.
- The hand-maintained index can fall out of date or acquire broken links.
- Immutability is inconvenient when a decision was merely poorly worded; the remedy is a
  superseding ADR, not an edit.
- The bootstrap ADRs are partly retrospective and less detailed than records written at decision
  time.

### Follow-ups

- Add "ADR written or superseded" to the definition of done for architectural changes.
- Review the index and links at each release.
- Write ADRs for the open decisions that block implementation, starting with the A/B update
  mechanism, the agent language, and the UI stack.
- Link relevant ADRs from component READMEs and from code comments where a non-obvious constraint
  originates in a decision.

## Alternatives considered

- No formal record. Rejected: rationale would live only in chat history and commit messages.
- A wiki or external notes tool. Rejected: drifts from the repository, is not reviewed with code,
  and is not available to offline agents.
- A heavyweight RFC process with formal review periods. Rejected as disproportionate for a small
  project; it would slow decisions without adding much traceability.
- Recording decisions only in commit messages. Rejected: hard to discover, no standard structure,
  and poor at capturing alternatives and consequences.
- Full MADR. Rejected: more sections than this project will maintain; the lightweight subset keeps
  the useful parts (context, decision, consequences, alternatives).

## References

- Michael Nygard, "Documenting architecture decisions":
  <https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions>
- ADR organization and resources: <https://adr.github.io/>
- MADR project: <https://github.com/adr/madr>
- [Initial design session](../notes/2026-11-14-initial-design-session.md)
