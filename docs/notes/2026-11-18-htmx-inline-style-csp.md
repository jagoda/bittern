# HTMX inline style blocked by the content security policy

- Date: 2026-11-18
- Status: Recorded defect; policy and HTMX configuration unchanged
- Related:
  [BIT-034](../plan/implementation-plan.md#bit-034-remove-the-htmx-inline-style-csp-console-error),
  [ADR 0019](../adr/0019-ui-stack.md), [UI README](../../ui/README.md),
  [browser check](../../tests/browser/README.md)

## Observed error

A Playwright Chromium run against the browser-check daemon reports this console error on every page
that loads HTMX:

```text
Applying inline style violates the following Content Security Policy directive 'style-src 'self''. Either the 'unsafe-inline' keyword, a hash ('sha256-faU7yAF8NxuMTNEwVmBz+VcYeIoBQ2EMHW3WaVxCvnk='), or a nonce ('nonce-...') is required to enable inline execution. The action has been blocked.
```

The page continues to render and the status panel refresh works. The error is pre-existing and is
not caused by the status layout change.

## Directive and cause

The directive is `style-src 'self'`, emitted by `securityHeaders` in
[`agent/internal/ui/ui.go`](../../agent/internal/ui/ui.go). The full policy is self-only for
scripts, styles, images, connections, forms and frames.

HTMX 2.0.11 at [`ui/static/htmx-2.0.11.min.js`](../../ui/static/htmx-2.0.11.min.js) injects a
`<style>` element into the document head at startup for its `.htmx-indicator` and `.htmx-request`
rules. `includeIndicatorStyles` defaults to true. An injected `<style>` element is an inline style
under CSP, so Chromium blocks it. No current template uses an HTMX indicator, which is why the
blocked rules have no visible effect today. A future indicator would silently not appear, and the
console error would remain on every HTMX page.

## Candidate remedies

Neither remedy is chosen here. The ticket owns the decision and the evidence for it.

1. Allow inline styles for `style-src`.
   - Change: add `'unsafe-inline'` to the `style-src` directive. A hash or nonce allowance would be
     narrower, but each has its own upkeep: a hash must match HTMX's injected text exactly, and a
     nonce must be wired through the HTMX config and the response headers.
   - Trade-off: clears the error for all pages and any injected style, including styles not under
     Bittern's control. It weakens the style boundary that the rest of the policy keeps tight.
2. Configure HTMX to stop injecting indicator styles.
   - Change: set `includeIndicatorStyles` to false through the HTMX meta configuration before the
     script loads, and put the needed `.htmx-indicator` and `.htmx-request` rules in
     `ui/static/bittern.css`.
   - Trade-off: keeps `style-src` strict and keeps all styling in the local stylesheet. It adds a
     dependency on a vendored HTMX configuration option and on Bittern maintaining the equivalent
     rules; a later HTMX version that injects other styles would bring the error back.

## Next step

[BIT-034](../plan/implementation-plan.md#bit-034-remove-the-htmx-inline-style-csp-console-error)
owns the fix and the regression check. Do not add `'unsafe-inline'` to `script-src`, and do not
change the policy as part of an unrelated UI change.
