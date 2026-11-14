# LEDG-12: Ledger balances drift from the sum of postings

Type: Bug
Reporter: Ines Duarte (Finance)
Updated: 2026-10-28

## Description

Month-end reconciliation found accounts whose `GET /accounts/{id}/balance`
differs from the sum of their postings by one cent or more after a few
thousand entries. Finance needs balances that are exact. The ledger holds
USD and EUR (2 decimals), JPY (0 decimals) and KWD (3 decimals), and will
add currencies.

## Acceptance criteria

1. Posting and summing any number of entries gives the exact decimal
   balance in every supported currency.
2. Existing entries keep their recorded value; nothing is lost in the move.
3. The mobile app and the partner reporting export keep working during the
   rollout; they can switch to a new field in a later release.
