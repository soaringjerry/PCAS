-- A diagnostic execution lease bounds automatic calls, independently of body
-- retention. Manual prepared/delivered packages have no automatic execution.
ALTER TABLE context_attempts ADD COLUMN execution_expires_at timestamptz;
UPDATE context_attempts SET execution_expires_at=created_at+interval '5 minutes'
 WHERE observation_layer<>'manual_package';
ALTER TABLE context_attempts ADD CONSTRAINT context_attempt_execution_lease
 CHECK ((observation_layer='manual_package')=(execution_expires_at IS NULL)
 AND (execution_expires_at IS NULL OR execution_expires_at>=created_at));
CREATE INDEX context_attempts_execution_expiry_idx ON context_attempts(execution_expires_at)
 WHERE observation_layer<>'manual_package' AND state IN('prepared','dispatched');
