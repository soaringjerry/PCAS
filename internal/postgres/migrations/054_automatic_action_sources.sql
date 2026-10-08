-- Automatic creation uses the existing action audit and undo path.
ALTER TABLE action_log DROP CONSTRAINT action_log_source_check;
ALTER TABLE action_log ADD CONSTRAINT action_log_source_check
 CHECK(source IN('command','desk','worker','background_extraction','background_topic'));
