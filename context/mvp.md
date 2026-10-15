# Tripline — MVP Definition

This document is the single source of truth the triage engine reasons against.
If a ticket cannot be justified by something written here, it is not MVP.

## Product

Tripline lets a group of friends plan a trip together: one shared itinerary,
one place to track who paid for what.

## Deadline

**Investor demo day — Friday 13 November 2026.** Seed round. The last
working sprint before the demo is TRIP Sprint 6 (2–13 Nov). Nothing ships
after that date counts.

## Golden path (what the investors will watch)

1. An organiser signs up and creates a trip (name, destination, dates).
2. They share an invite link; four friends join from their phones.
3. The group adds flights, stays and activities to a shared day-by-day itinerary
   and votes on the options.
4. Someone logs an expense; it is split between members and the app shows who
   owes whom.
5. Members are notified by email when they are invited and when the trip changes.

**Demo success metric:** a five-person group completes the golden path on mobile
web in under three minutes with zero errors.

## Non-negotiables (keep regardless of size)

- Anything without which a golden-path step cannot be completed.
- Security and legal obligations: password reset, rate limiting on public
  endpoints, privacy consent (Australian Privacy Act — we collect emails).
- Stability of the demo: error monitoring and a load test of the golden path.

## Explicitly out of scope (V2)

- Native iOS / Android apps — mobile web only.
- AI / ML features of any kind (recommendations, generation, sentiment).
- Real-time collaboration. Polling every 30 seconds is acceptable.
- Multi-currency and live FX. AUD only.
- Internationalisation. English only.
- Booking, payments, partner search APIs (flights, hotels).
- Admin analytics, referral, loyalty and other growth features.
- Architecture changes not driven by a measured problem (microservices,
  caching layers, etc.).

## Team & constraints

- 4 engineers, 2-week sprints, Monday–Friday.
- Stack: Next.js on Firebase Hosting, Go API on Cloud Run, Cloud SQL Postgres,
  Firebase Auth.
- Velocity is measured from closed sprints on the TRIP board; do not assume
  more than the trailing average.

## Triage rules

1. **Golden-path test.** A ticket is MVP only if removing it breaks a
   golden-path step or a non-negotiable.
2. **Split, don't just cut.** If a ticket mixes MVP and non-MVP acceptance
   criteria, keep the MVP slice as a new ticket and move the rest to V2.
3. **Dependencies travel together.** A ticket that blocks an MVP ticket is MVP.
   A ticket only needed by V2 work is V2.
4. **Priority labels are inputs, not decisions.** A "Highest" priority does not
   override the golden-path test.
5. **Capacity is a hard limit.** If MVP scope still exceeds capacity after
   cutting, flag the deadline as at risk rather than silently overcommitting.
