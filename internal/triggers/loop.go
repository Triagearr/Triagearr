package triggers

// Triggers share pollers.TickLoop for their immediate-tick + on-interval
// scheduling — the semantics are identical and the duplication was a hazard.
