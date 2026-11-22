# Camera selection fills its own form

The camera step's radio looked inert. Selecting one changed nothing until the owner pressed Use this
camera, and choosing a radio without pressing the button then submitting Review produced validation
errors. The maintainer asked for the selection itself to fill the form, without a new JavaScript
file and without a second primary action.

## What changed

- The candidate radio issues an HTMX GET for the same camera route with `selected=<address>`. The
  response is the camera identity fragment, not the full page, and it replaces `#camera-identity`.
- The fragment fills the name, address and hardware address. The name is the reported vendor and
  model, or `Camera N` in list order when neither is useful. With HTMX, the current name and a hidden
  marker travel with the request, so a name the owner typed is kept and an unchanged generated name
  can follow a new selection.
- The hardware address field is outside the identity target, so the fragment returns it as an
  out-of-band swap. The disclosure stays as it is.
- The secondary Use this camera button remains visible for a browser without HTMX or JavaScript. It
  submits the same GET and renders the full page. It is deliberately not hidden behind `noscript`,
  because an HTMX asset that fails while JavaScript is enabled would otherwise remove the only way
  to complete the step.
- A status line names the camera that filled the form and points at Review. The only primary action
  remains Review configuration.

## Bulk add, OQ-30

The row is still a label containing one named input, so a checkbox can replace the radio without
rebuilding the row. The list form can carry one Add selected cameras action in place of the fallback
button. That alone is not bulk add: [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing)
still has to collect every selected address, place the cameras in the list at the top of the step,
collect credentials per camera, and decide the review and apply atomicity. That work is separate
from this change and remains behind [OQ-30](../open-questions.md).

## Verification

`make ui-test`, `make check`, `make design-check` and `make ui-browser-check` pass with 0 failures
and 0 warnings. The selection state was driven at 360 by 800 and 1280 by 800; the no-JavaScript
fallback was driven separately and filled the same three fields.
