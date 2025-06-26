# Metadata Backfill Tool

## Overview

The `pcasctl backfill` command is a utility for retroactively adding metadata to existing vector embeddings in the PostgreSQL database. This is necessary when:

- You have existing vector embeddings created before metadata fields were added
- The metadata extraction logic has been updated
- You need to migrate from an older version of PCAS

## How It Works

The backfill process:

1. Reads events from the SQLite database (fact storage)
2. Extracts metadata fields from each event
3. Updates the corresponding vector embedding in PostgreSQL with the metadata
4. Processes events in batches for efficiency

## Usage

```bash
pcasctl backfill --sqlite /path/to/events.db --postgres "postgresql://user:pass@localhost/pcas"
```

### Options

- `--sqlite, -s`: Path to the SQLite database file containing events (required)
- `--postgres, -p`: PostgreSQL connection URI for the vector database (required)
- `--batch-size, -b`: Number of events to process per batch (default: 100)
- `--dry-run, -d`: Preview what would be updated without making changes

### Examples

1. **Basic backfill**:
   ```bash
   pcasctl backfill -s ~/.pcas/events.db -p "postgresql://localhost/pcas"
   ```

2. **Dry run to preview changes**:
   ```bash
   pcasctl backfill -s ~/.pcas/events.db -p "postgresql://localhost/pcas" --dry-run
   ```

3. **Custom batch size for large datasets**:
   ```bash
   pcasctl backfill -s ~/.pcas/events.db -p "postgresql://localhost/pcas" -b 500
   ```

## Metadata Fields

The following metadata fields are extracted and stored:

- `event_type`: The type of the event
- `event_source`: The source system that generated the event
- `timestamp_unix`: Unix timestamp of the event (as string)
- `timestamp`: RFC3339 formatted timestamp
- `user_id`: User identifier (if present)
- `session_id`: Session identifier (if present)
- `trace_id`: Trace identifier for correlation (if present)
- `correlation_id`: Direct correlation identifier (if present)

## Performance Considerations

- The tool processes events in batches to reduce database round trips
- Progress is logged every 10 successful updates and every 1000 processed events
- Failed updates are logged but don't stop the process
- You can safely re-run the command to retry failed updates

## Error Handling

Common reasons for update failures:

1. **No corresponding vector**: The event exists in SQLite but has no vector embedding in PostgreSQL
2. **Database connectivity**: Network issues or database unavailability
3. **Permission issues**: Insufficient privileges to update records

The tool will continue processing even if some updates fail, and provides a summary at the end showing:
- Total events processed
- Successfully updated count
- Error count

## Integration with Hybrid Search

Once metadata is backfilled, the hybrid search functionality becomes available for all historical data:

- User-specific filtering in RAG
- Time-based queries
- Event type filtering
- Session-based context retrieval

This enables the full power of PCAS's hybrid memory system on your existing data.