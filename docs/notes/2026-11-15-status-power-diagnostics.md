# Status-page power actions and diagnostics packet

This note records the two status-page tickets and keeps them separate from the camera onboarding
tickets. It is not an ADR or a plan change.

Related: [implementation plan](../plan/implementation-plan.md),
[BIT-010](../plan/implementation-plan.md#bit-010-factory-reset-and-support-bundle-skeleton),
[BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress),
[BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux),
[BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing).

- [BIT-030](../plan/implementation-plan.md#bit-030-status-page-reboot-and-shutdown-actions) adds
  reboot and shutdown to the status page. It uses the BIT-014 operation view, an administrator
  session with CSRF protection, a confirmation that names the effect on Frigate, recording, and
  storage, and a durable audit event.
- [BIT-031](../plan/implementation-plan.md#bit-031-diagnostics-packet-download) adds the redacted
  diagnostics packet built from the BIT-010 support bundle and the BIT-014 operation record.

The tickets sit after the near-term clean-install work in the plan queue. BIT-031 starts after
BIT-010 and BIT-014 land.

The camera UX findings remain [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux)
and [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing).
The numbers were reused by separate branches. BIT-030 and BIT-031 are the status-page work, not a
renumbering of the camera tickets.
