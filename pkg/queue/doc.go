// Package queue provides a persistent download queue.
//
// A queue file belongs to exactly one process. Concurrent access from multiple
// application instances is not supported. Adding an existing file ID replaces
// its record, while deleting an unknown ID is a successful no-op.
package queue
