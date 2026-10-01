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
              "waitingFor": {"type": ["string", "null"]}
            },
            "required": ["op", "title", "due", "remind", "project", "notes", "owedTo", "waitingFor"],
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
                  "notesAppend": {"type": ["string", "null"], "description": "null 表示不修改此字段。"}
                },
                "required": ["title", "due", "remind", "project", "status", "notesAppend"],
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
              "kind": {"type": "string", "enum": ["plan", "draft", "breakdown", "summary", "ask"]},
              "prompt": {"type": "string"}
            },
            "required": ["op", "ref", "title", "kind", "prompt"],
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
    }
  },
  "required": ["reply", "used", "links", "show", "remember", "actions", "ask"],
  "additionalProperties": false
}`)
