package postgres

import "encoding/json"

// secretaryOutputSchema is fixed, like secretaryInstructions, and applies only
// to Codex secretary turns. Required nullable fields preserve the existing
// default reminders and partial updates; server validation still owns actions.
var secretaryOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "reply": {"type": "string"},
    "used": {"type": "array", "items": {"type": "string"}},
    "links": {"type": "array", "items": {"type": "string"}},
    "show": {"type": "array", "items": {"type": "string"}},
    "remember": {"type": "boolean"},
    "missingKeyInfo": {"type": "boolean"},
    "actions": {
      "type": "array",
      "items": {
        "anyOf": [
          {
            "type": "object",
            "properties": {
              "op": {"type": "string", "enum": ["create_task"]},
              "title": {"type": "string"},
              "due": {"type": ["string", "null"]},
              "remind": {"type": ["string", "null"]},
              "project": {"type": ["string", "null"]},
              "notes": {"type": ["string", "null"]},
              "owedTo": {"type": ["string", "null"]},
              "waitingFor": {"type": ["string", "null"]},
              "urgent": {"type": ["boolean", "null"]}
            },
            "required": ["op", "title", "due", "remind", "project", "notes", "owedTo", "waitingFor", "urgent"],
            "additionalProperties": false
          },
          {
            "type": "object",
            "properties": {
              "op": {"type": "string", "enum": ["update"]},
              "ref": {"type": "string"},
              "set": {
                "type": "object",
                "properties": {
                  "title": {"type": ["string", "null"], "description": "null 表示不修改此字段。"},
                  "due": {"type": ["string", "null"], "description": "null 表示不修改此字段。"},
                  "remind": {"type": ["string", "null"], "description": "null 表示不修改此字段。"},
                  "project": {"type": ["string", "null"], "description": "null 表示不修改此字段。"},
                  "status": {"type": ["string", "null"], "description": "null 表示不修改此字段。"},
                  "notesAppend": {"type": ["string", "null"], "description": "null 表示不修改此字段。"},
                  "urgent": {"type": ["boolean", "null"], "description": "null 表示不修改此字段。"}
                },
                "required": ["title", "due", "remind", "project", "status", "notesAppend", "urgent"],
                "additionalProperties": false
              }
            },
            "required": ["op", "ref", "set"],
            "additionalProperties": false
          },
          {
            "type": "object",
            "properties": {
              "op": {"type": "string", "enum": ["create_idea"]},
              "title": {"type": "string"},
              "condition": {"type": ["string", "null"]},
              "conditionDue": {"type": ["string", "null"]},
              "project": {"type": ["string", "null"]}
            },
            "required": ["op", "title", "condition", "conditionDue", "project"],
            "additionalProperties": false
          },
          {
            "type": "object",
            "properties": {"op": {"type": "string", "enum": ["create_project"]}, "name": {"type": "string"}},
            "required": ["op", "name"],
            "additionalProperties": false
          },
          {
            "type": "object",
            "properties": {
              "op": {"type": "string", "enum": ["add_steps"]},
              "ref": {"type": "string"},
              "steps": {"type": "array", "items": {"type": "string"}}
            },
            "required": ["op", "ref", "steps"],
            "additionalProperties": false
          },
          {
            "type": "object",
            "properties": {
              "op": {"type": "string", "enum": ["delegate"]},
              "ref": {"type": "string"},
              "title": {"type": "string"},
              "kind": {"type": "string", "enum": ["plan", "draft", "breakdown", "summary", "ask", "revise"]},
              "prompt": {"type": "string"},
              "documentId": {"type": ["string", "null"]},
              "baseVersion": {"type": ["integer", "null"]}
            },
            "required": ["op", "ref", "title", "kind", "prompt", "documentId", "baseVersion"],
            "additionalProperties": false
          }
        ]
      }
    },
    "ask": {
      "anyOf": [
        {"type": "null"},
        {
          "type": "object",
          "properties": {"question": {"type": "string"}, "options": {"type": "array", "items": {"type": "string"}}},
          "required": ["question", "options"],
          "additionalProperties": false
        }
      ]
    },
    "memoryPlan": {
      "anyOf": [
        {"type": "null"},
        {
          "type": "object",
          "properties": {
            "depth": {"type": "string", "enum": ["light", "medium", "heavy"]},
            "groups": {"type": "array", "items": {"type": "string"}},
            "mentioned": {"type": "array", "items": {"type": "string"}},
            "adopted": {"type": "array", "items": {"type": "string"}}
          },
          "required": ["depth", "groups", "mentioned", "adopted"],
          "additionalProperties": false
        }
      ]
    }
  },
  "required": ["reply", "used", "links", "show", "remember", "missingKeyInfo", "actions", "ask", "memoryPlan"],
  "additionalProperties": false
}`)

// The schema is written out in full above and must stay that way. A provider
// that constrains output follows the order the properties are written in, and
// each action form is told apart by "op", which therefore comes first.
// Rebuilding this schema from a decoded map sorts the keys: "op" then led only
// in update and add_steps, and every create_task came back as one of those.
