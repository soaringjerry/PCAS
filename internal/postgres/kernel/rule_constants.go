package kernel

import (
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
)

const (
	CompareInterval    = time.Minute
	CompareStage       = "memory.compare"
	EntityCompareStage = "memory.entity_compare"
	ComparePriority    = 8
	CompareLimit       = 200
)

// Entity identity rules advance independently of claim comparison rules.
const EntityCompareVersion = 2

// Version 3 reprocesses previously organized rule memories only.
// Other version-2 classifications remain valid without relabeling.
var OrganizeVersion = 3

const (
	OrganizeInterval           = time.Minute
	OrganizeStage              = organize.Stage
	OrganizePriority           = 4
	OrganizeBatchLimit         = 40
	OrganizeCompareHourlyLimit = 140 // contract 4.1: 118 across the 2.6 stages + 10 project_handover + 10 effort, 2 topic_project
)

const (
	StatusInterval   = time.Minute
	CardStage        = "memory.card"
	HandoverStage    = "memory.handover"
	CardPriority     = ComparePriority
	SelfCardPriority = ComparePriority - 1
	HandoverPriority = 14
)

const CompareVersion = 2

var CardVersion = 1
