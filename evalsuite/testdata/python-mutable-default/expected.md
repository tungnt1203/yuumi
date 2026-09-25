# Expected: python-mutable-default

The PR adds `apply_discounts(cart, discounts=[])`: the default argument is a
LIST (mutable), and the function also calls `discounts.append("applied")` on
that same default object when the caller does not pass its own `discounts`.
The default value is SHARED and accumulates data across calls (a classic
Python bug), unlike `Cart.__init__` just above, which handles it correctly
(`None`, then a new list inside).

The bot MUST catch:

- [ ] A finding about a mutable default argument in `apply_discounts`.
- [ ] The finding is on `cart.py`, at `def apply_discounts(cart, discounts=[]):`.
- [ ] The `suggestion` (if any) follows the `Cart.__init__` pattern in the
      same file: `discounts=None`, then `if discounts is None: discounts = []`.

It should not report `Cart.__init__`/`add`: they are unchanged in this PR
and already handle defaults correctly.
