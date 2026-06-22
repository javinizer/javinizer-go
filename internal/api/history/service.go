package history

// HistoryService was removed — it was a shallow pass-through over HistoryRepositoryInterface.
// API handlers now call database.HistoryRepositoryInterface directly, except for stats
// which delegates to history.Logger.GetStats (which aggregates 6 repo calls and has
// genuine depth).
