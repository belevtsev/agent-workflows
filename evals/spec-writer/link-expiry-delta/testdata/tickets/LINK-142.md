# LINK-142: Shorter default lifetime and owner extension for short links

Type: Story
Status: Ready for spec
Component: link expiry
Reporter: Mei Tanaka (Trust & Safety)
Updated: 2026-10-20

## Background

Abuse reports show that most phishing short links are used within their first
week. A 30-day default keeps them alive three weeks longer than legitimate
campaigns need.

## Acceptance criteria

1. A link created without an explicit expiry expires 7 days after it is
   created. The expired response stays HTTP 410 Gone.
2. Links created before the release keep the expiry they already have.
3. The owner of a link can extend it with `POST /links/{code}/extend` and a
   JSON body `{"days": N}`, where N is a whole number from 1 to 90 inclusive.
   A successful call returns HTTP 200 with the new `expires_at`.
4. An extension request with N outside 1 to 90, or not a whole number,
   returns HTTP 400 and leaves the expiry unchanged.
5. A request to extend a link by anyone other than its owner returns HTTP 403
   and leaves the expiry unchanged.
6. Expired links can be extended too, so an owner can revive a campaign link.

## Notes

- This is a breaking change for API clients that rely on the 30-day default;
  it must be called out in the release notes.
- Explicit `expires_at` behaviour does not change.
