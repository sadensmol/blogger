package domain

type RunID int64

type RunKind string

const (
	RunKindDigestMorning RunKind = "digest-morning"
	RunKindDigestShow    RunKind = "digest-show"
	RunKindFetch         RunKind = "fetch"
	RunKindProfileInit   RunKind = "profile-init"
	RunKindProfileUpdate RunKind = "profile-update"
	RunKindDiscovery     RunKind = "discovery"
)

type RunStatus string

const (
	RunStatusRunning RunStatus = "running"
	RunStatusOK      RunStatus = "ok"
	RunStatusFailed  RunStatus = "failed"
)
