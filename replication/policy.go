package replication

// FIXME: Leftovers from previous versions, not used currently
type InitialReplPolicy string

const (
	InitialReplPolicyMostRecent InitialReplPolicy = "most_recent"
	InitialReplPolicyAll        InitialReplPolicy = "all"
)

const DEFAULT_INITIAL_REPL_POLICY = InitialReplPolicyMostRecent
