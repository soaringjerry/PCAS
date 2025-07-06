# PCAS Tagging Ontology Guide

**Version**: 0.1

**Status**: Proposed

## 1. Introduction

This document defines the official, recommended Tagging Ontology for the PCAS ecosystem. A rich, consistent, and standardized tagging system is the cornerstone of PCAS's memory and RAG (Retrieval Augmented Generation) capabilities. The better事件are tagged, the smarter PCAS becomes.

All d-App developers are strongly encouraged to adhere to this guide when publishing events to the PCAS bus.

## 2. Core Principles

1.  **Structured (Key-Value)**: All tags exist as key-value pairs within the `attributes` field of an `Event`. We do not use simple, valueless tags.
2.  **Namespaced**: To avoid collisions and provide clarity, complex tag keys should use a simple namespace prefix, separated by a dot. E.g., `location.type`, `location.name`.
3.  **Hierarchical**: The tagging system is designed to be hierarchical, allowing for queries एट multiple levels of granularity (e.g., searching by `location.country` vs. `location.city`).
4.  **Extensible**: This guide defines the core ontology. D-Apps are free to add their own custom, domain-specific tags. It is recommended to prefix these custom tags with the d-App's name (e.g., `dreamscribe.notebook_id`).

## 3. Core Ontology Vocabulary

This section defines the first version of the officially recommended tag vocabulary.

### 3.1 Realm

The highest-level domain for an event.

*   **Key**: `realm`
*   **Values**:
    *   `personal`: Related to personal life, family, hobbies.
    *   `work`: Related to professional work, projects, colleagues.
    *   `education`: Related to learning, courses, schools.
    *   `health`: Related to fitness, medical records, appointments.

### 3.2 Location

Describes the physical location where an event occurred.

*   **Keys**:
    *   `location.type`: The category of the location.
    *   `location.name`: The specific name of the location.
    *   `location.gps`: (Optional) GPS coordinates.
*   **Example Values**:
    *   `location.type`: `home`, `office`, `school`, `supermarket`, `gym`
    *   `location.name`: `MIT`, `Walmart Supercenter #1234`

### 3.3 Activity

Describes the user's activity when the event occurred.

*   **Key**: `activity.type`
*   **Values**: `meeting`, `studying`, `shopping`, `exercising`, `driving`

### 3.4 Project & Task

For goal-oriented events.

*   **Keys**:
    *   `project.id`: A unique identifier for a project.
    *   `task.id`: A unique identifier for a task within a project.
*   **Example Values**:
    *   `project.id`: `pcas-v0.2-docs`
    *   `task.id`: `adr-003-finalization`

## 4. Best Practices Example

An event generated while taking notes during a specific lecture in DreamScribe should be tagged comprehensively:

```json
"attributes": {
  "realm": "education",
  "location.type": "school",
  "location.name": "MIT",
  "activity.type": "studying",
  "project.id": "cs101-final-paper",
  "course.id": "6.001",
  "lecture.number": "2",
  "session_id": "session-6.001-lecture2-20250706",
  "dreamscribe.notebook_id": "nb-advanced-recursion"
}
```

By following this guide, we ensure that all d-Apps contribute to building a single, unified, and incredibly rich knowledge graph for the user.

## 5. Leveraging Tags for Retrieval

Tagging is only useful if the tags can be used to retrieve information efficiently. The PCAS `EventBusService` provides a powerful mechanism to do this via the `Search` RPC.

When calling the `Search` method, you can use the `attribute_filters` field to pre-filter the search space *before* the semantic search engine runs. This dramatically improves both the speed and relevance of search results.

### 5.1 Code Example (Go SDK)

Imagine you want to find all notes related to the "CS101" course to provide context for a summary. Instead of a broad semantic search, you can scope it down precisely.

```go
import (
    "context"
    "log"

    "github.com/soaringjerry/pcas/pkg/sdk/go"
    busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
)

func findCourseNotes(client *sdk.Client, courseID string) (*busv1.SearchResponse, error) {
    ctx := context.Background()

    req := &busv1.SearchRequest{
        QueryText: "notes about recursion", // The semantic part of the query
        TopK:      10,
        // The crucial part: filtering by attributes
        AttributeFilters: map[string]string{
            "realm":     "education",
            "course.id": courseID,
        },
    }

    log.Printf("Searching for notes in course %s...", courseID)
    resp, err := client.Search(ctx, req)
    if err != nil {
        return nil, err
    }

    log.Printf("Found %d relevant notes.", len(resp.Events))
    return resp, nil
}
```

By providing the `AttributeFilters`, you instruct PCAS to only perform the semantic search on events that are already tagged with `realm: "education"` AND `course.id: "cs101"`. This is the cornerstone of building intelligent, context-aware d-Apps.