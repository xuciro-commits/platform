package platformserver

// One application, end to end, across two environments: a
// definition is drafted and frozen in dev, activated, used by an operator,
// promoted as sealed bytes into prod, its records migrated, then upgraded to a
// v2 that adds one optional scalar — on both environments, through the
// reviewed plan — and finally recovered from a snapshot plus the journal tail.
// Nothing here is a new engine: the test strings the existing candidate,
// release, environment, transfer and recovery owners together and asserts the
// seams between them hold.
