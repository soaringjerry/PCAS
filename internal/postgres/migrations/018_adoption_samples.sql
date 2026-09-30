-- Adoption samples belong to the same undoable action as the adopted result.
CREATE TRIGGER log_sample_change AFTER INSERT OR UPDATE OR DELETE ON training_samples
FOR EACH ROW EXECUTE FUNCTION collect_action_change();
