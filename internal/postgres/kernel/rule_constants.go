package kernel

import "time"

const (
	CompareInterval = time.Minute
	ComparePriority = 8
)

const (
	OrganizeInterval = time.Minute
	OrganizePriority = 4
)

const (
	StatusInterval   = time.Minute
	CardPriority     = ComparePriority
	SelfCardPriority = ComparePriority - 1
	HandoverPriority = 14
)

var CardVersion = 1
