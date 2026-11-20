package kernel

import "time"

const (
	CompareInterval = time.Minute
	ComparePriority = 8
	CompareLimit    = 200
)

// Entity identity rules advance independently of claim comparison rules.
const EntityCompareVersion = 2

// Version 3 reprocesses previously organized rule memories only.
// Other version-2 classifications remain valid without relabeling.
var OrganizeVersion = 3

const (
	OrganizeInterval   = time.Minute
	OrganizePriority   = 4
	OrganizeBatchLimit = 40
)

const (
	StatusInterval   = time.Minute
	CardPriority     = ComparePriority
	SelfCardPriority = ComparePriority - 1
	HandoverPriority = 14
)

const CompareVersion = 2

var CardVersion = 1
