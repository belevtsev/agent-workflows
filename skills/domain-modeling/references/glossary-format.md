# Glossary format

```md
# {Context name}

{One or two sentences on what this context is and why it exists.}

## Language

**Invoice**:
A request for payment sent to a customer after delivery.
_Avoid_: Bill, payment request

**Customer**:
A person or organization that places orders.
_Avoid_: Client, buyer, account
```

## Rules

- Keep definitions to one or two sentences. Define what a term is, not what it does.
- Include only terms specific to this context. Timeouts, retries, and error types are general programming concepts and don't belong, however often the project uses them.
- Group terms under subheadings when clusters emerge.

## Several contexts

Most repositories have one `GLOSSARY.md` at the root. When contexts use the same word differently, give each its own glossary and add a `GLOSSARY-MAP.md` at the root:

```md
# Glossary map

## Contexts

- [Ordering](./internal/ordering/GLOSSARY.md): receives and tracks customer orders
- [Billing](./internal/billing/GLOSSARY.md): generates invoices and processes payments

## Relationships

- **Ordering to Billing**: Ordering emits `OrderShipped`, and Billing consumes it to generate invoices
```

When several contexts exist, work out which one the topic belongs to, and ask if it's unclear.
